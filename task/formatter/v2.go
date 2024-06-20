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

// bkunifylogbeat默认格式

package formatter

import (
	"github.com/TencentBlueKing/bkmonitor-datalink/pkg/libgse/beat"
	"github.com/TencentBlueKing/bkunifylogbeat/config"
	"github.com/TencentBlueKing/bkunifylogbeat/utils"
	"github.com/elastic/beats/filebeat/util"
	"strings"
)

type v2Formatter struct {
	taskConfig *config.TaskConfig
}

type LineItem struct {
	Data           string `json:"data"`
	IterationIndex int    `json:"iterationindex"`
}

// NewV2Formatter bkunifylogbeat日志采集输出格式
func NewV2Formatter(config *config.TaskConfig) (*v2Formatter, error) {
	f := &v2Formatter{
		taskConfig: config,
	}
	return f, nil
}

// Format  最新格式兼容
func (f v2Formatter) Format(events []*util.Data) beat.MapStr {
	var (
		datetime, utcTime string
		timestamp         int64
	)
	datetime, utcTime, timestamp = utils.GetDateTime()

	lastState := events[len(events)-1].GetState()
	filename := lastState.Source
	if len(f.taskConfig.RemovePathPrefix) > 0 {
		filename = strings.TrimPrefix(filename, f.taskConfig.RemovePathPrefix)
	}
	data := beat.MapStr{
		"dataid":   f.taskConfig.DataID,
		"filename": filename,
		"datetime": datetime,
		"utctime":  utcTime,
		"time":     timestamp,
	}

	hasEvent := false

	if events[0].Event.HasTexts() {
		itemsCount := 0
		for _, event := range events {
			itemsCount += event.Event.Count()
		}
		items := make([]LineItem, 0, itemsCount)
		index := 0
		for _, event := range events {
			for _, text := range event.Event.Texts {
				if text == "" {
					continue
				}
				items = append(items, LineItem{Data: text, IterationIndex: index})
				index += 1
			}
		}
		data["items"] = items
		hasEvent = len(items) > 0
	} else {
		var items []beat.MapStr
		for index, event := range events {
			item := event.Event.Fields.Clone()
			if item == nil {
				continue
			}
			item["iterationindex"] = index
			items = append(items, item)
		}
		data["items"] = items
		hasEvent = len(items) > 0
	}

	// 仅需要更新采集状态的事件数
	if !hasEvent {
		return nil
	}

	//发送正常事件
	if f.taskConfig.ExtMeta != nil {
		data["ext"] = f.taskConfig.ExtMeta
	} else {
		data["ext"] = map[string]interface{}{}
	}
	return data
}

func init() {
	for _, name := range []string{"v2", "default"} {
		err := FormatterRegister(name, func(config *config.TaskConfig) (Formatter, error) {
			return NewV2Formatter(config)
		})
		if err != nil {
			panic(err)
		}
	}
}
