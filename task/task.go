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

package task

import (
	"fmt"
	cfg "github.com/TencentBlueKing/bkunifylogbeat/config"
	"github.com/TencentBlueKing/bkunifylogbeat/task/base"
	"github.com/TencentBlueKing/bkunifylogbeat/task/input"
	"github.com/TencentBlueKing/collector-go-sdk/v2/bkbeat/beat"
	"github.com/TencentBlueKing/collector-go-sdk/v2/bkbeat/bkmonitoring"
	"github.com/TencentBlueKing/collector-go-sdk/v2/bkbeat/logp"
	"github.com/elastic/beats/filebeat/input/file"
	"github.com/elastic/beats/libbeat/monitoring"
)

var (
	crawlerSendTotal = bkmonitoring.NewInt("crawler_send_total")
)

// Task 采集任务具体实现，负责filebeat采集事件处理、过滤、打包，并发送到采集框架
type Task struct {
	*base.Node

	Config   *cfg.TaskConfig
	beatDone chan struct{}

	input *input.Input

	crawlerSendTotal *monitoring.Int //正常事件总数
}

// NewTask 生成采集任务实例
func NewTask(config *cfg.TaskConfig, beatDone chan struct{}, lastStates []file.State) (*Task, error) {
	task := &Task{
		Node: &base.Node{
			ID: config.ID,

			In:   make(chan interface{}),
			Outs: make(map[string]chan interface{}),

			End: make(chan struct{}),
		},

		Config:   config,
		beatDone: beatDone,
	}
	task.crawlerSendTotal = bkmonitoring.NewIntWithDataID(config.DataID, "crawler_send_total")

	var err error
	task.input, err = input.GetInput(task.Config, task.Node, task.End, lastStates)
	if err != nil {
		return nil, fmt.Errorf("[%s] error while get input: %s", task.ID, err)
	}
	logp.L.Infof("init task finish. task Map is:", task.Node)

	// 开启输出监听，持续监听数据输入
	go task.Run()

	return task, nil
}

// Start 负责启动采集任务实例
func (task *Task) Start() {
	// 开启输入
	task.input.Start()
}

func (task *Task) Run() {
	for {
		select {
		case <-task.beatDone:
			logp.L.Infof("beat(%s) is done", task.ID)
			return
		case <-task.End:
			logp.L.Infof("task(%s) is done", task.ID)
			return
		case event := <-task.In:
			crawlerSendTotal.Add(1)
			task.crawlerSendTotal.Add(1)
			beat.SendEvent(event.(beat.Event))
		}
	}
}

// Stop 负责停止采集任务实例，在Filebeat采集插件停止后退出
func (task *Task) Stop() error {
	logp.L.Infof("task(%s) is Stop", task.ID)
	task.ParentNode.RemoveOutput(task.Node)
	task.crawlerSendTotal.Set(0)
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
