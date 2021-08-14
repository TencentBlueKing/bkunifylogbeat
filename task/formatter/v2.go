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
	"github.com/TencentBlueKing/bkunifylogbeat/config"
	"github.com/TencentBlueKing/bkunifylogbeat/task"
	"github.com/TencentBlueKing/bkunifylogbeat/utils"
	"github.com/TencentBlueKing/collector-go-sdk/v2/bkbeat/beat"
	"github.com/elastic/beats/filebeat/util"
)

type v2Formatter struct {
	taskConfig *config.TaskConfig
}

//NewV2Formatter: bkunifylogbeat日志采集输出格式
func NewV2Formatter(config *config.TaskConfig) (*v2Formatter, error) {
	f := &v2Formatter{
		taskConfig: config,
	}
	return f, nil
}

//Format: 最新格式兼容
func (f v2Formatter) Format(events []*util.Data) beat.MapStr {
	var (
		datetime, utcTime string
		timestamp         int64
	)
	datetime, utcTime, timestamp = utils.GetDateTime()

	lastEvent := events[len(events)-1]
	data := beat.MapStr{
		"dataid":   GetDataIdForFormatter(lastEvent, f.taskConfig),
		"filename": GetFilenameForFormatter(lastEvent, f.taskConfig),
		"datetime": datetime,
		"utctime":  utcTime,
		"time":     timestamp,
	}

	hasEvent := false

	var items []beat.MapStr
	for index, event := range events {
		item := event.Event.Fields
		if item == nil {
			continue
		}
		hasEvent = true
		item["iterationindex"] = index
		if f.taskConfig.Type == config.UDP_INPUT {
			item["data"] = item["message"]
			for _, key := range []string{"message", "log"} {
				delete(item, key)
			}
		}
		if f.taskConfig.Type == config.OTLP_INPUT {
			item["data"] = item["message"]
			for _, key := range []string{"message", "trace_id"} {
				delete(item, key)
			}
		}
		items = append(items, item)
	}
	// 仅需要更新采集状态的事件数
	if !hasEvent {
		return nil
	}
	data["items"] = items

	//发送正常事件
	if f.taskConfig.ExtMeta != nil {
		data["ext"] = f.taskConfig.ExtMeta
	} else {
		data["ext"] = ""
	}
	return data
}

func init() {
	for _, name := range []string{"v2", "default"} {
		err := task.FormatterRegister(name, func(config *config.TaskConfig) (task.Formatter, error) {
			return NewV2Formatter(config)
		})
		if err != nil {
			panic(err)
		}
	}
}
