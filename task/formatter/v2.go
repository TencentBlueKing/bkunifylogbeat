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
	"encoding/json"
	"github.com/TencentBlueKing/bkunifylogbeat/config"
	"github.com/TencentBlueKing/bkunifylogbeat/task"
	"github.com/TencentBlueKing/bkunifylogbeat/utils"
	"github.com/TencentBlueKing/collector-go-sdk/v2/bkbeat/beat"
	"github.com/TencentBlueKing/collector-go-sdk/v2/bkbeat/logp"
	"github.com/elastic/beats/filebeat/util"
	"strings"
)

type ContainerStdoutFields struct {
	Log    string `json:"log"`
	Stream string `json:"stream"`
	Time   string `json:"time"`
}

type v2Formatter struct {
	taskConfig *config.TaskConfig
}

// NewV2Formatter bkunifylogbeat日志采集输出格式
func NewV2Formatter(config *config.TaskConfig) (*v2Formatter, error) {
	f := &v2Formatter{
		taskConfig: config,
	}
	return f, nil
}

// syslogFormatter 兼容syslog数据格式
func syslogFormatter(events []*util.Data) []*util.Data {
	for _, event := range events {
		fields := event.Event.Fields
		hasKey, err := fields.HasKey("syslog")
		if err != nil {
			continue
		}

		// 校验syslog是否满足rfc3164格式
		logData := beat.MapStr{}
		if hasKey {
			logData["message"] = fields["message"]

			// source ip and port info
			address := fields["log"].(beat.MapStr)["source"].(beat.MapStr)["address"].(string)
			arr := strings.Split(address, ":")
			if len(arr) > 0 {
				logData["log_source_ip"] = arr[0]
			} else {
				logData["log_source_ip"] = ""
			}
			if len(arr) > 1 {
				logData["log_source_port"] = arr[1]
			} else {
				logData["log_source_port"] = ""
			}

			// host info
			hostName, err := fields.GetValue("hostname")
			if err == nil {
				logData["hostname"] = hostName
			} else {
				logData["hostname"] = ""
			}

			// syslog、event、process info
			logData["syslog"] = fields["syslog"]
			logData["event"] = fields["event"]
			logData["process"] = fields["process"]
		} else {
			logData["message"] = fields["message"]
		}

		// 适配日志平台，将json转化成str
		jf, err := json.Marshal(logData)
		if err != nil {
			logp.L.Errorf("Error starting the server, %v", err)
			continue
		}
		sf := string(jf)

		event.Event.Fields = beat.MapStr{
			"data": sf,
		}
	}
	return events
}

// Format 最新格式兼容
func (f v2Formatter) Format(events []*util.Data) beat.MapStr {

	// 兼容syslog采集上报数据
	if f.taskConfig.Type == "syslog" {
		events = syslogFormatter(events)
	}

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

	var items []beat.MapStr
	for index, event := range events {
		item := event.Event.Fields
		if item == nil {
			continue
		}
		hasEvent = true
		item["iterationindex"] = index
		if f.taskConfig.IsContainerStd {
			content, ok := item["data"].(string)
			if ok {
				jsonContent := ContainerStdoutFields{}
				e := json.Unmarshal([]byte(content), &jsonContent)
				if e != nil {
					logp.L.Errorf("output format error, container stdout no json format, data(%v)", content)
				}
				item["data"] = jsonContent.Log
				item["stream"] = jsonContent.Stream
				item["log_time"] = jsonContent.Time
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
		data["ext"] = map[string]interface{}{}
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
