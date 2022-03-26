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
	"github.com/TencentBlueKing/bkunifylogbeat/config"
	"github.com/elastic/beats/libbeat/beat"
	process "github.com/elastic/beats/libbeat/processors"
)

// Processors : 兼容数据平台过滤规则
type Processors struct {
	taskConfig     *config.TaskConfig
	processors     *process.Processors
	filterMaxIndex int
}

// NewProcessors : 兼容原采集器处理并复用filebeat.processors
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

	return processors, nil
}

// Run : 处理采集事件
func (client *Processors) Run(event *beat.Event) *beat.Event {
	if event.Fields == nil {
		return event
	}

	if client.processors != nil {
		event := client.processors.Run(event)
		if event == nil {
			return nil
		}
	}
	return event
}
