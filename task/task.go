// Tencent is pleased to support the open source community by making bkunifylogbeat 蓝鲸日志采集器 available.
//
// Copyright (C) 2021 THL A29 Limited, a Tencent company.  All rights reserved.
//
// bkunifylogbeat 蓝鲸日志采集器 is licensed under the MIT License.
//
// License for bkunifylogbeat 蓝鲸日志采集器:
// --------------------------------------------------------------------
// Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated
// documentation files (the "Software"), to deal in the Software without restriction, including without limitation
// the rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the Software,
// and to permit persons to whom the Software is furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all copies or substantial
// portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT
// LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN
// NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY,
// WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE
// SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.

// task manager
// 1. task pipeline
// 2. input runner

package task

import (
	"fmt"
	"runtime"
	"sync"
	"time"

	cfg "github.com/TencentBlueKing/bkunifylogbeat/config"
	"github.com/TencentBlueKing/bkunifylogbeat/task/filter"
	"github.com/TencentBlueKing/bkunifylogbeat/task/input"
	"github.com/TencentBlueKing/bkunifylogbeat/task/processor"
	"github.com/TencentBlueKing/bkunifylogbeat/task/sender"
	"github.com/TencentBlueKing/bkunifylogbeat/utils"
	"github.com/TencentBlueKing/collector-go-sdk/v2/bkbeat/beat"
	"github.com/TencentBlueKing/collector-go-sdk/v2/bkbeat/bkmonitoring"
	"github.com/TencentBlueKing/collector-go-sdk/v2/bkbeat/logp"

	"github.com/elastic/beats/filebeat/channel"
	"github.com/elastic/beats/filebeat/input/file"
	"github.com/elastic/beats/filebeat/util"
	"github.com/elastic/beats/libbeat/common"
	"github.com/elastic/beats/libbeat/monitoring"
)

var (
	inputFailed      = bkmonitoring.NewInt("task_input_failed")
	processorsFailed = bkmonitoring.NewInt("task_processors_failed")
	senderFailed     = bkmonitoring.NewInt("task_sender_failed")

	crawlerReceived  = bkmonitoring.NewInt("crawler_received")
	crawlerState     = bkmonitoring.NewInt("crawler_state")
	crawlerSendTotal = bkmonitoring.NewInt("crawler_send_total")
	crawlerDropped   = bkmonitoring.NewInt("crawler_dropped")

	isEnableRateLimiter bool            // 是否开启速率限制
	cpuLimiter          *utils.CPULimit // CPU使用率限制
)

// SetResourceLimit 在一定程度上限制CPU使用
func SetResourceLimit(maxCpuLimit, checkTimes int) {
	numCPU := runtime.NumCPU()
	// 在docker富容器中 并且 开启了速率限制
	if utils.IsInDocker() {
		logp.L.Infof("enable rate limit. because numOfCpu(%d) && isInDocker(%v)",
			numCPU, true,
		)

		if maxCpuLimit > 0 && maxCpuLimit <= numCPU*100 {
			cpuLimiter = utils.NewCPULimit(maxCpuLimit, checkTimes)
			isEnableRateLimiter = true
		} else {
			logp.L.Infof("disable rate limit. because cpu limit config(%d) is not valid",
				maxCpuLimit,
			)
		}
	} else {
		logp.L.Infof("disable rate limit. because numOfCpu(%d) && isInDocker(%v), "+
			"cpu limit config(%d)",
			numCPU, false, maxCpuLimit,
		)
		if cpuLimiter != nil {
			cpuLimiter.Stop()
		}
		isEnableRateLimiter = false
	}
}

// Task 采集任务具体实现，负责filebeat采集事件处理、过滤、打包，并发送到采集框架
type Task struct {
	ID       string
	config   *cfg.TaskConfig
	beatDone chan struct{}

	input      *input.Input
	filters    *filter.Filters
	processors *processor.Processors
	sender     *sender.Sender

	done chan struct{}
	wg   sync.WaitGroup

	crawlerReceived  *monitoring.Int //state事件
	crawlerState     *monitoring.Int //state事件
	crawlerSendTotal *monitoring.Int //正常事件总数
	crawlerDropped   *monitoring.Int //过滤掉的事件总数
}

// NewTask 生成采集任务实例
func NewTask(config *cfg.TaskConfig, beatDone chan struct{}) *Task {
	task := &Task{
		ID:       config.ID,
		config:   config,
		beatDone: beatDone,
		done:     make(chan struct{}),
	}
	task.crawlerReceived = bkmonitoring.NewIntWithDataID(config.DataID, "crawler_received")
	task.crawlerState = bkmonitoring.NewIntWithDataID(config.DataID, "crawler_state")
	task.crawlerSendTotal = bkmonitoring.NewIntWithDataID(config.DataID, "crawler_send_total")
	task.crawlerDropped = bkmonitoring.NewIntWithDataID(config.DataID, "crawler_dropped")
	return task
}

// Start 负责启动采集任务实例
func (task *Task) Start(lastStates []file.State) error {
	var err error

	// init sender
	s, err := sender.NewSender(task.config, task.done, beat.SendEvent)
	if err != nil {
		senderFailed.Add(1)
		return fmt.Errorf("[%s] error while initializing sender: %s", task.ID, err)
	}
	task.sender = s
	task.sender.Start()

	// init filters
	task.filters = filter.NewFilters(task.config)

	// init input processors
	task.processors, err = processor.NewProcessors(task.config)
	if err != nil {
		processorsFailed.Add(1)
		return fmt.Errorf(": %s", err)
	}
	task.wg.Add(1)
	p, err := input.New(task.config.RawConfig, ConnectToTask(task), task.beatDone, lastStates, nil)
	if err != nil {
		inputFailed.Add(1)
		return fmt.Errorf("[%s] error while initializing input: %s", task.ID, err)
	}
	task.input = p
	task.input.Start()
	return nil
}

// Stop 负责停止采集任务实例，在Filebeat采集插件停止后退出
func (task *Task) Stop() error {
	task.input.Stop()
	task.wg.Wait()
	task.crawlerState.Set(0)
	task.crawlerSendTotal.Set(0)
	task.crawlerDropped.Set(0)
	return nil
}

// Reload 通知各采集模块针对重载操作进行适配
func (task *Task) Reload() error {
	task.input.Reload()
	return nil
}

// Close 由Filebeat在停止采集插件后调用
func (task *Task) Close() error {
	task.wg.Done()
	close(task.done)
	return nil
}

// Done 返回任务状态channel
func (task *Task) Done() <-chan struct{} {
	return task.done
}

// OnEvent 处理input runner发送的事件
func (task *Task) OnEvent(data *util.Data) bool {
	if data == nil {
		logp.L.Errorf("task get event nil, task_id:%s", task.ID)
		return false
	}
	event := &data.Event
	select {
	case <-task.beatDone:
	case <-task.done:
		return false
	default:
	}

	//接收到的事件
	task.crawlerReceived.Add(1)
	crawlerReceived.Add(1)

	if event.Fields == nil {
		//采集进度类事件
		task.crawlerState.Add(1)
		crawlerState.Add(1)
	} else {
		// 处理速率限制，可在一定层度上限制CPU的使用率
		if isEnableRateLimiter && cpuLimiter != nil {
			checkInterval := cpuLimiter.GetCheckInterval()
			for {
				if cpuLimiter.Allow() {
					break
				} else {
					time.Sleep(checkInterval)
				}
			}
		}

		event = task.filters.Run(event)
		if event != nil {
			event = task.processors.Run(event)
		}

		if event != nil {
			//正常事件
			task.crawlerSendTotal.Add(1)
			crawlerSendTotal.Add(1)
			data.Event = *event
		} else {
			//需要丢弃的事件
			data.Event.Fields = nil
			task.crawlerDropped.Add(1)
			crawlerDropped.Add(1)
		}
	}
	return task.sender.OnEvent(data)
}

// String 任务实例名称
func (task *Task) String() string {
	return fmt.Sprintf("task [type=>%s, ID=>%s]", task.config.Type, task.ID)
}

// ConnectToTask 返回任务实例，用于接收采集事件 OnEvent
func ConnectToTask(task *Task) channel.Connector {
	return func(cfg *common.Config, m *common.MapStrPointer) (channel.Outleter, error) {
		return task, nil
	}
}
