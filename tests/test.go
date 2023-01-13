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

package tests

import (
	"github.com/TencentBlueKing/bkmonitor-datalink/pkg/libgse/beat"
	bkmonitoring "github.com/TencentBlueKing/bkmonitor-datalink/pkg/libgse/monitoring"
	"github.com/TencentBlueKing/bkunifylogbeat/config"
	"github.com/TencentBlueKing/bkunifylogbeat/task/base"
	"github.com/elastic/beats/filebeat/input/file"
	"github.com/elastic/beats/filebeat/util"
)

// MockLogEvent : 生成日志采集事件
func MockLogEvent(source string, content string) *util.Data {
	data := &util.Data{
		Event: beat.Event{
			Fields: beat.MapStr{
				"data": content,
			},
		},
	}
	if content == "" {
		data.Event.Fields = nil
	}
	data.SetState(file.State{
		Source: source,
		Offset: 1,
	})
	return data
}

// MockTaskNode : 生成TaskNode
func MockTaskNode(config *config.TaskConfig) *base.TaskNode {
	return &base.TaskNode{
		Node: &base.Node{
			ID: config.ID,

			In:   make(chan interface{}),
			Outs: make(map[string]chan interface{}),

			End: make(chan struct{}),

			GameOver: make(chan struct{}),

			TaskNodeList: map[string]map[string]*base.TaskNode{},
		},

		// Crawler metrics
		CrawlerReceived:  bkmonitoring.NewIntWithDataID(config.DataID, "crawler_received"),
		CrawlerState:     bkmonitoring.NewIntWithDataID(config.DataID, "crawler_state"),
		CrawlerSendTotal: bkmonitoring.NewIntWithDataID(config.DataID, "crawler_send_total"),
		CrawlerDropped:   bkmonitoring.NewIntWithDataID(config.DataID, "crawler_dropped"),

		// sender metrics
		SenderReceive:   bkmonitoring.NewIntWithDataID(config.DataID, "sender_received"),
		SenderState:     bkmonitoring.NewIntWithDataID(config.DataID, "sender_state"),
		SenderSendTotal: bkmonitoring.NewIntWithDataID(config.DataID, "sender_send_total"),
	}
}
