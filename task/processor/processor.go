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

package processor

import (
	"fmt"
	"sync"

	"github.com/TencentBlueKing/bkmonitor-datalink/pkg/libgse/logp"
	bkmonitoring "github.com/TencentBlueKing/bkmonitor-datalink/pkg/libgse/monitoring"
	"github.com/TencentBlueKing/bkunifylogbeat/config"
	"github.com/TencentBlueKing/bkunifylogbeat/task/base"
	"github.com/TencentBlueKing/bkunifylogbeat/task/sender"
	"github.com/elastic/beats/filebeat/util"
	"github.com/elastic/beats/libbeat/beat"
	process "github.com/elastic/beats/libbeat/processors"
)

var (
	processorsMaps = map[string]*Processors{}
	mtx            sync.RWMutex

	numOfProcessTotal = bkmonitoring.NewInt("num_processors_total") // 当前全局processors的数量

	processDroppedTotal = bkmonitoring.NewInt("processors_dropped_total")
	processHandledTotal = bkmonitoring.NewInt("processors_handled_total")
)

// Processors : 兼容数据平台过滤规则
type Processors struct {
	*base.Node

	processors *process.Processors
}

// GetProcessors : 获取processor
func GetProcessors(taskCfg *config.TaskConfig, taskNode *base.TaskNode) (*Processors, error) {
	var (
		ok bool
		p  *Processors
	)

	func() {
		mtx.RLock()
		defer mtx.RUnlock()
		p, ok = processorsMaps[taskCfg.ProcessorID]
	}()

	if ok {
		send, err := sender.GetSender(taskCfg, taskNode)
		if err != nil {
			return nil, err
		}

		err = p.MergeProcessorsConfig(taskCfg)
		if err != nil {
			return nil, err
		}

		p.AddOutput(send.Node)
		p.AddTaskNode(send.Node, taskNode)
		return p, nil
	}
	return NewProcessors(taskCfg, taskNode)
}

// NewProcessors : 新建processor
func NewProcessors(taskCfg *config.TaskConfig, taskNode *base.TaskNode) (*Processors, error) {
	var err error
	var p = &Processors{
		Node: base.NewEmptyNode(taskCfg.ProcessorID),

		processors: nil,
	}
	err = p.MergeProcessorsConfig(taskCfg)
	if err != nil {
		return nil, err
	}

	send, err := sender.NewSender(taskCfg, taskNode)
	if err != nil {
		return nil, err
	}
	p.AddOutput(send.Node)
	p.AddTaskNode(send.Node, taskNode)

	go p.Run()

	logp.L.Infof("add processors(%s) to global processorsMaps", p.ID)
	mtx.Lock()
	defer mtx.Unlock()
	processorsMaps[taskCfg.ProcessorID] = p
	numOfProcessTotal.Add(1)
	return p, nil
}

// RemoveProcessors : 移除全局缓存
func RemoveProcessors(id string) {
	logp.L.Infof("remove processors(%s) in global processorsMaps", id)
	mtx.Lock()
	defer mtx.Unlock()
	delete(processorsMaps, id)
	numOfProcessTotal.Add(-1)
}

// MergeProcessorsConfig : 合并多个任务的Processor配置
// 理论上Merge这里不存在任何动作，因为Processor的配置是一样的
func (p *Processors) MergeProcessorsConfig(taskCfg *config.TaskConfig) error {
	var err error
	if p.processors == nil && taskCfg.Processors != nil {
		p.processors, err = process.New(taskCfg.Processors)
		if err != nil {
			return fmt.Errorf("create libbeat.processors faied, err=>%v", err)
		}
	}
	return nil
}

// Run : 循环处理数据
func (p *Processors) Run() {
	defer close(p.GameOver)
	defer RemoveProcessors(p.ID)
	for {
		select {
		case <-p.End:
			// node is done
			return
		case e := <-p.In:
			data := e.(*util.Data)
			event := p.Handle(&data.Event)
			if event != nil {
				for _, out := range p.Outs {
					select {
					case <-p.End:
						logp.L.Infof("node processor(%s) is done", p.ID)
						return
					case out <- data:
						processHandledTotal.Add(1)
					}
				}
			} else {
				processDroppedTotal.Add(1)
				for _, taskNodeList := range p.TaskNodeList {
					for _, tNode := range taskNodeList {
						base.CrawlerDropped.Add(1)
						tNode.CrawlerDropped.Add(1)
					}
				}
			}
		}
	}
}

// Handle : 处理采集事件
func (p *Processors) Handle(event *beat.Event) *beat.Event {
	if event.Fields == nil {
		return event
	}

	if p.processors != nil {
		event := p.processors.Run(event)
		if event == nil {
			return nil
		}
	}
	return event
}
