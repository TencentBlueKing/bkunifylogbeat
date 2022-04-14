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

package filter

import (
	"strings"
	"sync"

	"github.com/TencentBlueKing/bkunifylogbeat/config"
	"github.com/TencentBlueKing/bkunifylogbeat/task/base"
	"github.com/TencentBlueKing/bkunifylogbeat/task/processor"
	"github.com/TencentBlueKing/collector-go-sdk/v2/bkbeat/beat"
	"github.com/TencentBlueKing/collector-go-sdk/v2/bkbeat/bkmonitoring"
	"github.com/TencentBlueKing/collector-go-sdk/v2/bkbeat/logp"
	"github.com/elastic/beats/filebeat/util"
)

var (
	filterMaps = map[string]*Filters{}
	mtx        sync.RWMutex

	numOfFilterTotal = bkmonitoring.NewInt("num_filter_total") // 当前全局filter的数量

	filterDroppedTotal = bkmonitoring.NewInt("filter_dropped_total") // 被过滤的总数
	filterHandledTotal = bkmonitoring.NewInt("filter_handled_total") // 被处理的总数
)

type Filters struct {
	*base.Node

	Delimiter      string
	filterMaxIndex int

	taskConfigMaps map[string]*config.TaskConfig
}

// GetFilters get filter
func GetFilters(taskCfg *config.TaskConfig, taskNode *base.TaskNode) (*Filters, error) {
	var (
		ok  bool
		fil *Filters
	)

	func() {
		mtx.RLock()
		defer mtx.RUnlock()
		fil, ok = filterMaps[taskCfg.FilterID]
	}()

	if ok {
		p, err := processor.GetProcessors(taskCfg, taskNode)
		if err != nil {
			return nil, err
		}

		fil.MergeFilterConfig(taskCfg)
		fil.AddOutput(p.Node)
		fil.AddTaskNode(p.Node, taskNode)
		return fil, nil
	}
	return NewFilters(taskCfg, taskNode)
}

func NewFilters(taskCfg *config.TaskConfig, taskNode *base.TaskNode) (*Filters, error) {
	var err error
	var fil = &Filters{
		Node:      base.NewEmptyNode(taskCfg.FilterID),
		Delimiter: taskCfg.Delimiter,

		taskConfigMaps: map[string]*config.TaskConfig{},
	}
	fil.MergeFilterConfig(taskCfg)

	p, err := processor.NewProcessors(taskCfg, taskNode)
	if err != nil {
		return nil, err
	}
	fil.AddOutput(p.Node)
	fil.AddTaskNode(p.Node, taskNode)

	go fil.Run()

	logp.L.Infof("add filter(%s) to global filterMaps", fil.ID)
	mtx.Lock()
	defer mtx.Unlock()
	filterMaps[taskCfg.FilterID] = fil
	numOfFilterTotal.Add(1)
	return fil, nil
}

// RemoveFilter : 移除全局缓存
func RemoveFilter(id string) {
	logp.L.Infof("remove filter(%s) in global filterMaps", id)
	mtx.Lock()
	defer mtx.Unlock()
	delete(filterMaps, id)
	numOfFilterTotal.Add(-1)
}

func (f *Filters) MergeFilterConfig(taskCfg *config.TaskConfig) {
	if taskCfg.HasFilter {
		for _, filConfig := range taskCfg.Filters {
			if len(filConfig.Conditions) != 0 {
				maxIndex := filConfig.Conditions[len(filConfig.Conditions)-1].Index
				if f.filterMaxIndex < maxIndex {
					f.filterMaxIndex = maxIndex
				}
			}
		}
	}
	f.taskConfigMaps[taskCfg.ProcessorID] = taskCfg
}

func (f *Filters) Run() {
	defer RemoveFilter(f.ID)
	for {
		select {
		case <-f.End:
			// node is done
			return
		case e := <-f.In:
			data := e.(*util.Data)
			event := &data.Event

			var text string
			var ok bool
			text, ok = event.Fields["data"].(string)
			if !ok || f.Delimiter == "" {
				for _, out := range f.Outs {
					select {
					case <-f.End:
						logp.L.Infof("node filter(%s) is done", f.ID)
						return
					case out <- data:
						filterHandledTotal.Add(1)
					}
				}
				break
			}

			// index为N时，数组切分最少需要分成N+1段
			words := strings.SplitN(text, f.Delimiter, f.filterMaxIndex+1)
			for i := range words {
				words[i] = strings.TrimSpace(words[i])
			}
			for processorID, taskConfig := range f.taskConfigMaps {
				event := f.Handle(words, text, taskConfig, event)
				if event == nil {
					// update metric
					{
						filterDroppedTotal.Add(1)
						taskNodeList, ok := f.TaskNodeList[processorID]
						if ok {
							for _, tNode := range taskNodeList {
								base.CrawlerDropped.Add(1)
								tNode.CrawlerDropped.Add(1)
							}
						}
					}
					continue
				}

				out, ok := f.Outs[processorID]
				if ok {
					select {
					case <-f.End:
						logp.L.Infof("node filter(%s) is done", f.ID)
						return
					case out <- data:
						filterHandledTotal.Add(1)
					}
				}
			}

		}
	}
}

// Handle 过滤数据
func (f *Filters) Handle(words []string, text string, taskConfig *config.TaskConfig, event *beat.Event) *beat.Event {
	if !taskConfig.HasFilter {
		return event
	}

	for _, filterConfig := range taskConfig.Filters {
		access := true
		for _, condition := range filterConfig.Conditions {
			// 匹配第n列，如果n小于等于0，则变更为整个字符串包含
			if condition.Index <= 0 {
				if !strings.Contains(text, condition.Key) {
					access = false
					break
				} else {
					continue
				}
			}
			operationFunc := getOperation(condition.Op)
			if operationFunc != nil {
				if len(words) < condition.Index {
					access = false
					break
				}
				if !operationFunc(words[condition.Index-1], condition.Key) {
					access = false
					break
				}
			}
		}
		if access {
			return event
		}
	}
	return nil
}
