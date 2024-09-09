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

// 兼容bklogbeat格式

package formatter

import (
	"strings"

	"github.com/TencentBlueKing/bkmonitor-datalink/pkg/libgse/beat"
	"github.com/TencentBlueKing/bkunifylogbeat/config"
	"github.com/TencentBlueKing/bkunifylogbeat/utils"
	"github.com/elastic/beats/filebeat/util"
)

type v1Formatter struct {
	taskConfig *config.TaskConfig
}

// NewV1Formatter 兼容bklogbeat输出格式
func NewV1Formatter(config *config.TaskConfig) (*v1Formatter, error) {
	f := &v1Formatter{
		taskConfig: config,
	}
	return f, nil
}

type commonFormatter interface {
	GetTaskConfig() *config.TaskConfig
}

func (f v1Formatter) GetTaskConfig() *config.TaskConfig {
	return f.taskConfig
}

func prepareData(f commonFormatter, events []*util.Data) beat.MapStr {
	var (
		datetime, utcTime string
		timestamp         int64
	)
	datetime, utcTime, timestamp = utils.GetDateTime()

	lastState := events[len(events)-1].GetState()
	filename := lastState.Source
	if len(f.GetTaskConfig().RemovePathPrefix) > 0 {
		filename = strings.TrimPrefix(filename, f.GetTaskConfig().RemovePathPrefix)
	}
	data := beat.MapStr{
		"dataid":   f.GetTaskConfig().DataID,
		"filename": filename,
		"datetime": datetime,
		"utctime":  utcTime,
		"time":     timestamp,
	}

	return data
}

// Format bklogbeat输出格式兼容
func (f v1Formatter) Format(events []*util.Data) beat.MapStr {
	data := prepareData(f, events)

	var texts []string
	for _, event := range events {
		for _, text := range event.Event.GetTexts() {
			if text == "" {
				continue
			}
			texts = append(texts, text)
		}
	}

	// 仅需要更新采集状态的事件数
	if len(texts) == 0 {
		return nil
	}
	data["data"] = texts

	//发送正常事件
	if len(f.taskConfig.GetExtMeta()) > 0 {
		data["ext"] = f.taskConfig.ExtMeta
	} else {
		data["ext"] = ""
	}
	return data
}

func init() {
	err := FormatterRegister("v1", func(config *config.TaskConfig) (Formatter, error) {
		return NewV1Formatter(config)
	})
	if err != nil {
		panic(err)
	}
}
