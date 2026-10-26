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

// Package task 采集任务
package task

import (
	"fmt"
	"sync"

	"github.com/TencentBlueKing/bkmonitor-datalink/pkg/libgse/beat"
	"github.com/TencentBlueKing/bkmonitor-datalink/pkg/libgse/logp"
	bkmonitoring "github.com/TencentBlueKing/bkmonitor-datalink/pkg/libgse/monitoring"
	"github.com/TencentBlueKing/bkmonitor-datalink/pkg/libgse/output/bkpipe_multi"
	"github.com/elastic/beats/filebeat/input/file"
	"github.com/elastic/beats/libbeat/outputs"

	cfg "github.com/TencentBlueKing/bkunifylogbeat/config"
	"github.com/TencentBlueKing/bkunifylogbeat/task/base"
	"github.com/TencentBlueKing/bkunifylogbeat/task/input"
)

// Task 采集任务具体实现，负责filebeat采集事件处理、过滤、打包，并发送到采集框架
type Task struct {
	*base.TaskNode

	Config   *cfg.TaskConfig
	beatDone chan struct{}

	input *input.Input

	output outputs.Client

	// previewCompletion is set only for the -T direct-read task. It is called
	// once after the drain marker reaches the end of the task pipeline.
	previewCompletion func(error)
	previewOnce       sync.Once
}

// NewTask 生成采集任务实例
func NewTask(config *cfg.TaskConfig, beatDone chan struct{}, lastStates []file.State) (*Task, error) {
	task := &Task{
		TaskNode: &base.TaskNode{
			Node: base.NewEmptyNode(config.ID),

			// Crawler metrics
			CrawlerReceived:  bkmonitoring.NewIntWithDataID(config.DataID, "crawler_received"),
			CrawlerState:     bkmonitoring.NewIntWithDataID(config.DataID, "crawler_state"),
			CrawlerSendTotal: bkmonitoring.NewIntWithDataID(config.DataID, "crawler_send_total"),
			CrawlerDropped:   bkmonitoring.NewIntWithDataID(config.DataID, "crawler_dropped"),
			ExtractFailed:    bkmonitoring.NewIntWithDataID(config.DataID, "field_extraction_failed"),
			DedupDropped:     bkmonitoring.NewIntWithDataID(config.DataID, "dedup_dropped"),
			DedupEvictedKeys: bkmonitoring.NewIntWithDataID(config.DataID, "dedup_evicted_keys"),
			DedupFailOpen:    bkmonitoring.NewIntWithDataID(config.DataID, "dedup_fail_open"),

			// sender metrics
			SenderReceive:   bkmonitoring.NewIntWithDataID(config.DataID, "sender_received"),
			SenderState:     bkmonitoring.NewIntWithDataID(config.DataID, "sender_state"),
			SenderSendTotal: bkmonitoring.NewIntWithDataID(config.DataID, "sender_send_total"),
		},

		Config:   config,
		beatDone: beatDone,
	}

	var err error
	task.input, err = input.GetInput(task.Config, task.TaskNode, beatDone, lastStates)
	if err != nil {
		return nil, fmt.Errorf("[%s] error while get input: %s", task.ID, err)
	}
	logp.L.Infof("init task finish. task Map is: %v", task.Node)

	if task.Config.Output.Name() != "" {
		err = bkpipe_multi.RegisterTaskOutput(task.Config.ID, task.Config.Output)
		if err != nil {
			return nil, fmt.Errorf("[%s] error while register output: %s", task.ID, err)
		}
	}
	// 开启输出监听，持续监听数据输入
	go task.Run()

	return task, nil
}

// SetPreviewCompletion marks this task as the -T direct-read preview task and
// installs the completion callback used by the manager/LogBeat coordinator.
func (task *Task) SetPreviewCompletion(fn func(error)) {
	task.previewCompletion = fn
}

// StopInput asks the input producer to stop without closing the downstream
// Task nodes. Used by the signal path so the pipeline can still drain.
func (task *Task) StopInput() {
	if task.input != nil {
		task.input.GracefulStop()
	}
}

// Start 负责启动采集任务实例
func (task *Task) Start() {
	// 开启输入
	task.input.Start()
	if task.previewCompletion != nil {
		task.input.WatchCompletion(task.Config.ID)
	}
}

func (task *Task) Run() {
	defer close(task.GameOver)
	for {
		select {
		case <-task.beatDone:
			logp.L.Infof("beat(%s) is done", task.ID)
			return
		case <-task.End:
			logp.L.Infof("task(%s) is done", task.ID)
			return
		case event := <-task.In:
			if marker, ok := event.(*base.DrainMarker); ok {
				if marker.TaskID != task.Config.ID {
					continue
				}
				task.completePreview(marker.Err)
				return
			}
			base.CrawlerPackageSendTotal.Add(1)
			beatEvent := event.(beat.Event)
			beatEvent = bkpipe_multi.SetEventTaskID(beatEvent, task.Config.ID)
			beat.SendEvent(beatEvent)
		}
	}
}

func (task *Task) completePreview(err error) {
	task.previewOnce.Do(func() {
		if task.previewCompletion != nil {
			task.previewCompletion(err)
		}
	})
}

// Stop 负责停止采集任务实例，在Filebeat采集插件停止后退出
func (task *Task) Stop() error {
	logp.L.Infof("task(%s) is Stop", task.ID)
	// Task 退出只移除自己的诊断元数据；底层共享 input 可能仍在服务其他 Task。
	task.input.UnregisterAdaptiveScanTask(task.Config)
	task.ParentNode.RemoveOutput(task.Node)
	task.ParentNode.RemoveTaskNode(task.Node, task.TaskNode)

	if task.Config.Output.Name() != "" {
		bkpipe_multi.DeregisterTaskOutput(task.Config.ID)
	}

	logp.L.Infof("task(%s) is remove", task.ID)
	task.CloseOnce.Do(func() {
		close(task.End)
		task.WaitUntilGameOver() // 这里需要等待，确保全局共享变量已经完整清除相关节点
	})
	return nil
}

// Reload 通知各采集模块针对重载操作进行适配
func (task *Task) Reload() error {
	task.input.Reload()
	return nil
}

// String 任务实例名称
func (task *Task) String() string {
	return fmt.Sprintf("task [type=>%s, ID=>%s]", task.Config.Type, task.ID)
}
