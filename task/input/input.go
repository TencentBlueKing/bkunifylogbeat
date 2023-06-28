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

package input

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/TencentBlueKing/bkmonitor-datalink/pkg/libgse/beat"
	"github.com/TencentBlueKing/bkmonitor-datalink/pkg/libgse/logp"
	bkmonitoring "github.com/TencentBlueKing/bkmonitor-datalink/pkg/libgse/monitoring"
	"github.com/TencentBlueKing/bkunifylogbeat/config"
	"github.com/TencentBlueKing/bkunifylogbeat/task/base"
	"github.com/TencentBlueKing/bkunifylogbeat/task/filter"
	"github.com/TencentBlueKing/bkunifylogbeat/utils"
	"github.com/elastic/beats/filebeat/channel"
	"github.com/elastic/beats/filebeat/input"
	"github.com/elastic/beats/filebeat/input/file"
	"github.com/elastic/beats/filebeat/util"
	"github.com/elastic/beats/libbeat/common"
)

var (
	inputMaps = map[string]*Input{}
	mtx       sync.RWMutex

	numOfInputTotal = bkmonitoring.NewInt("task_input_total") // 当前全局input的数量

	// input 没有做处理，没有丢弃的可能，所以不上报这个指标
	//inputDroppedTotal = bkmonitoring.NewInt("input_dropped_total")
	inputHandledTotal = bkmonitoring.NewInt("input_handled_total")
)

type ContainerStdoutFields struct {
	Log    string `json:"log"`
	Stream string `json:"stream"`
	Time   string `json:"time"`
}

const (
	LogDelimiter = " "
	// LogTagPartial means the line is part of multiple lines.
	LogTagPartial = "P"
	// LogTagFull means the line is a single full line or the end of multiple lines.
	LogTagFull = "F"
	// LogTagDelimiter is the delimiter for different log tags.
	LogTagDelimiter = ":"
)

func GetInput(
	taskCfg *config.TaskConfig,
	taskNode *base.TaskNode,
	beatDone chan struct{},
	states []file.State,
) (*Input, error) {

	var (
		ok bool
		in *Input
	)
	func() {
		mtx.RLock()
		defer mtx.RUnlock()
		in, ok = inputMaps[taskCfg.InputID]
	}()

	if ok {
		f, err := filter.GetFilters(taskCfg, taskNode)
		if err != nil {
			return nil, err
		}
		in.AddOutput(f.Node)
		in.AddTaskNode(f.Node, taskNode)
		return in, nil
	}

	return NewInput(taskCfg, taskNode, beatDone, states)
}

func NewInput(
	taskCfg *config.TaskConfig,
	taskNode *base.TaskNode,
	beatDone chan struct{},
	states []file.State,
) (*Input, error) {
	var err error
	var in = &Input{
		Node:              base.NewEmptyNode(taskCfg.InputID),
		IsContainerStd:    taskCfg.IsContainerStd,
		IsCRIContainerStd: taskCfg.IsCRIContainerStd,
	}

	f, err := filter.NewFilters(taskCfg, taskNode)
	if err != nil {
		return nil, err
	}
	in.AddOutput(f.Node)
	in.AddTaskNode(f.Node, taskNode)
	go in.Run()

	// input.New 里会发送事件出来，需要先创建好后续的Output，再创建Input
	in.runner, err = input.New(
		taskCfg.RawConfig, ConnectToTask(in), beatDone, states, nil)
	if err != nil {
		return nil, err
	}

	logp.L.Infof("add input(%s) to global inputMaps", in.ID)
	mtx.Lock()
	defer mtx.Unlock()
	inputMaps[taskCfg.InputID] = in
	numOfInputTotal.Add(1)
	return in, nil
}

// RemoveInput : 移除全局缓存
func RemoveInput(id string) {
	logp.L.Infof("remove input(%s) in global inputMaps", id)
	mtx.Lock()
	defer mtx.Unlock()
	delete(inputMaps, id)
	numOfInputTotal.Add(-1)
}

type Input struct {
	*base.Node

	IsContainerStd    bool
	IsCRIContainerStd bool

	runner   *input.Runner
	runOnce  sync.Once
	stopOnce sync.Once
}

// Start 启动runner
// 创建后则自动启动
func (in *Input) Start() {
	in.runOnce.Do(func() {
		in.runner.Start()
	})
}

func (in *Input) Run() {
	defer close(in.GameOver)
	defer func() {
		RemoveInput(in.ID)
		in.stop()
	}()
	for {
		select {
		case <-in.End:
			return
		case e := <-in.In:
			// 处理速率限制，可在一定层度上限制CPU的使用率
			if utils.IsEnableRateLimiter && utils.GlobalCpuLimiter != nil {
				checkInterval := utils.GlobalCpuLimiter.GetCheckInterval()
				for {
					if utils.GlobalCpuLimiter.Allow() {
						break
					} else {
						time.Sleep(checkInterval)
					}
				}
			}

			base.CrawlerReceived.Add(1)

			data := e.(*util.Data)
			if data.Event.Fields != nil {
				in.normalizeContainerLog(data)
				for _, out := range in.Outs {
					select {
					case <-in.End:
						return
					case out <- data:
						inputHandledTotal.Add(1)
						for _, taskNodeList := range in.TaskNodeList {
							for _, tNode := range taskNodeList {
								tNode.CrawlerReceived.Add(1)
							}
						}
					}
				}
			} else {
				// 采集进度类事件
				beat.SendEvent(beat.Event{
					Fields:  nil,
					Private: data.GetState(),
				})
				base.CrawlerState.Add(1)
				for _, taskNodeList := range in.TaskNodeList {
					for _, tNode := range taskNodeList {
						tNode.CrawlerState.Add(1)
					}
				}
			}
		}
	}
}

// stop : 停止runner
// 停用的场景:
//  1. 当outs为空后，自动退出
//  2. 当End的channel被主动关闭后
func (in *Input) stop() {
	in.stopOnce.Do(func() {
		go in.runner.Stop() // 防止卡主reload的流程，这里改为异步，不等待input结束
	})
}

// Reload : Input不做reload处理，配置如果有变化，直接删除新建
func (in *Input) Reload() {
	return
}

// ConnectToTask 返回任务实例，用于接收采集事件 OnEvent
func ConnectToTask(in *Input) channel.Connector {
	return func(cfg *common.Config, m *common.MapStrPointer) (channel.Outleter, error) {
		return in, nil
	}
}

// Close 由Filebeat在停止采集插件后调用
func (in *Input) Close() error {
	return nil
}

// Done 返回任务状态channel
func (in *Input) Done() <-chan struct{} {
	return in.End
}

// OnEvent 处理input runner发送的事件
func (in *Input) OnEvent(data *util.Data) bool {
	if data == nil {
		logp.L.Errorf("task get event nil, inputID:%s", in.ID)
		return false
	}

	select {
	case <-in.End:
		return false
	case in.In <- data:
	}

	return true
}

// normalizeContainerLog 标准化容器日志故事
func (in *Input) normalizeContainerLog(data *util.Data) {
	item := data.Event.Fields

	if in.IsCRIContainerStd {
		content, ok := item["data"].(string)
		if ok {
			e := in.parseCRILog(content, item)
			if e != nil {
				logp.L.Errorf("output format error, container stdout no cri format, data(%s)", content)
			}
		}
	} else if in.IsContainerStd {
		content, ok := item["data"].(string)
		if ok {
			jsonContent := ContainerStdoutFields{}
			e := json.Unmarshal([]byte(content), &jsonContent)
			if e != nil {
				logp.L.Errorf("output format error, container stdout no json format, data(%s)", content)
			}
			item["data"] = jsonContent.Log
			item["stream"] = jsonContent.Stream
			item["log_time"] = jsonContent.Time
		}
	}
}

// parseCRILog parses logs in CRI log format. CRI Log format example:
//
//	2016-10-06T00:17:09.669794202Z stdout P log content 1
//	2016-10-06T00:17:09.669794203Z stderr F log content 2
func (in *Input) parseCRILog(log string, item beat.MapStr) error {
	// Parse timestamp
	idx := strings.Index(log, LogDelimiter)
	if idx < 0 {
		return fmt.Errorf("timestamp is not found")
	}
	item["log_time"] = log[:idx]

	// Parse stream type
	log = log[idx+1:]
	idx = strings.Index(log, LogDelimiter)
	if idx < 0 {
		return fmt.Errorf("stream type is not found")
	}
	item["stream"] = log[:idx]

	// Parse log tag
	log = log[idx+1:]
	idx = strings.Index(log, LogDelimiter)
	if idx < 0 {
		return fmt.Errorf("log tag is not found")
	}
	// Keep this forward compatible.
	tags := strings.Split(log[:idx], LogTagDelimiter)
	partial := tags[0] == LogTagPartial
	// Trim the tailing new line if this is a partial line.
	if partial && len(log) > 0 && log[len(log)-1] == '\n' {
		log = log[:len(log)-1]
	}

	// Get log content
	item["data"] = log[idx+1:]

	return nil
}
