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
	"strings"

	"github.com/TencentBlueKing/bkunifylogbeat/config"
	"github.com/elastic/beats/libbeat/beat"
	process "github.com/elastic/beats/libbeat/processors"
)

// Processors: 兼容数据平台过滤规则
type Processors struct {
	taskConfig     *config.TaskConfig
	processors     *process.Processors
	filterMaxIndex int
}

// NewProcessors: 兼容原采集器处理并复用filebeat.processors
func NewProcessors(config *config.TaskConfig) (*Processors, error) {
	processors := &Processors{
		taskConfig: config,
	}

	var err error
	if config.Processors != nil {
		processors.processors, err = process.New(config.Processors)
		if err != nil {
			return nil, fmt.Errorf("create libbeat.processors faied, err=>%v", err)
		}
	}

	// Filter
	if config.HasFilter {
		for _, f := range config.Filters {
			if len(f.Conditions) != 0 {
				if processors.filterMaxIndex < f.Conditions[len(f.Conditions)-1].Index {
					processors.filterMaxIndex = f.Conditions[len(f.Conditions)-1].Index
				}
			}
		}
	}

	return processors, nil
}

// Run: 处理采集事件
func (client *Processors) Run(event *beat.Event) *beat.Event {
	if event.Fields == nil {
		return event
	}

	// 原采集器过滤兼容
	if client.taskConfig.HasFilter {
		event = client.filter(event)
		if event == nil {
			return nil
		}
	}

	if client.processors != nil {
		event := client.processors.Run(event)
		if event == nil {
			return nil
		}
	}
	return event
}

// filter: 兼容原采集器过滤方式
func (client *Processors) filter(event *beat.Event) *beat.Event {
	// index为N时，数组切分最少需要分成N+1段
	var text string
	var ok bool
	if text, ok = event.Fields["data"].(string); !ok {
		return event
	}
	words := strings.SplitN(text, client.taskConfig.Delimiter, client.filterMaxIndex+1)

	for _, f := range client.taskConfig.Filters {
		access := true
		for _, condition := range f.Conditions {
			if condition.Index == -1 {
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
