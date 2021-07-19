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
	"sync"
	"time"

	cfg "github.com/TencentBlueKing/bkunifylogbeat/config"
	"github.com/TencentBlueKing/collector-go-sdk/v2/bkbeat/beat"
	"github.com/TencentBlueKing/collector-go-sdk/v2/bkbeat/bkmonitoring"
	"github.com/elastic/beats/filebeat/util"
	"github.com/elastic/beats/libbeat/monitoring"

	"github.com/TencentBlueKing/bkunifylogbeat/config"
	"github.com/TencentBlueKing/collector-go-sdk/v2/bkbeat/logp"
)

var (
	senderReceived  = bkmonitoring.NewInt("sender_received")
	senderState     = bkmonitoring.NewInt("sender_state")
	senderSendTotal = bkmonitoring.NewInt("sender_send_total")
)

// Sender: 对采集事件进行打包, 并调用beat发送事件
type Sender struct {
	taskConfig      *config.TaskConfig
	taskDone        chan struct{}
	cache           map[string][]*util.Data
	input           chan *util.Data
	wg              sync.WaitGroup
	publisher       PublisherFunc
	formatter       Formatter
	senderReceive   *monitoring.Int // 接收的事件数
	senderSendTotal *monitoring.Int // 发送到pipeline的数量
	senderState     *monitoring.Int // 仅需要更新采集状态的事件数(event.Field为空)
}

// Publisher: 接收采集事件并发送到outlet
type PublisherFunc func(beat.Event) bool

// NewSender 生成采集器Sender实例
func NewSender(config *cfg.TaskConfig, taskDone chan struct{}, publisher PublisherFunc) (*Sender, error) {
	sender := &Sender{
		taskConfig: config,
		taskDone:   taskDone,
		cache:      make(map[string][]*util.Data),
		input:      make(chan *util.Data),
		publisher:  publisher,
	}

	//formatter
	outputFormat := config.OutputFormat
	if outputFormat == "" {
		outputFormat = "default"
	}
	f, err := FindFormatterFactory(outputFormat)
	if err != nil {
		return nil, err
	}
	sender.formatter, err = f(config)
	if err != nil {
		return nil, err
	}

	//sender metrics
	sender.senderReceive = bkmonitoring.NewIntWithDataID(config.DataID, "sender_received")
	sender.senderState = bkmonitoring.NewIntWithDataID(config.DataID, "sender_state")
	sender.senderSendTotal = bkmonitoring.NewIntWithDataID(config.DataID, "sender_send_total")
	return sender, nil
}

// Start 启动Sender实例
func (client *Sender) Start() error {
	logp.L.Infof("Starting sender, %s", client.String())
	go client.run()
	return nil
}

// OnEvent获取采集事件
func (client *Sender) OnEvent(data *util.Data) bool {
	client.input <- data
	client.senderReceive.Add(1)
	senderReceived.Add(1)
	return true
}

func (client *Sender) run() error {
	senderDuration := 1 * time.Second
	senderTicker := time.NewTicker(senderDuration)

	defer func() {
		senderTicker.Stop()
	}()

	for {
		select {
		case <-client.taskDone:
			logp.L.Infof("sender quit, id: %s", client.String())
			return nil

		case <-senderTicker.C:
			// clear cache
			for _, buffer := range client.cache {
				if len(buffer) > 0 {
					client.send(buffer)
				}
			}
			client.cache = make(map[string][]*util.Data)

		case event := <-client.input:
			err := client.cacheSend(event)
			if err != nil {
				logp.L.Errorf("send event error, %v", err)
				continue
			}
		}
	}

	return nil
}

// Wait
func (client *Sender) Wait() {
	client.wg.Wait()
}

// Sender 实例名称
func (client *Sender) String() string {
	return fmt.Sprintf("Sender-TaskID-%s", client.taskConfig.ID)
}

func (client *Sender) cacheSend(event *util.Data) error {
	source := event.GetState().Source

	if !client.taskConfig.CanPackage {
		return client.send([]*util.Data{event})
	}

	buffer, exist := client.cache[source]
	// 特殊事件直接发送
	if event.Event.Fields == nil {
		if exist {
			buffer = append(buffer, event)
			client.send(buffer)
			client.cache[source] = []*util.Data{}
			return nil
		}
		client.send([]*util.Data{event})
		return nil
	}

	//正常事件处理
	if exist {
		client.cache[source] = append(buffer, event)
	} else {
		client.cache[source] = []*util.Data{event}
	}

	// if msg count reach max count, clear cache
	if len(client.cache[source]) >= client.taskConfig.PackageCount {
		client.send(client.cache[source])
		// clear cache
		client.cache[source] = []*util.Data{}
	}
	return nil
}

// send: 调用beat.SendEvent发送打包后的采集事件
func (client *Sender) send(events []*util.Data) error {
	if len(events) == 0 {
		return nil
	}

	lastState := events[len(events)-1].GetState()
	data := client.formatter.Format(events)
	//处理状态事件
	if data == nil {
		client.senderState.Add(1)
		senderState.Add(1)
		client.publisher(beat.Event{
			Fields:  nil,
			Private: lastState,
		})
		return nil
	}

	// send data
	client.publisher(beat.Event{
		Fields:  data,
		Private: lastState,
	})
	// 发送到pipeline的数量
	client.senderSendTotal.Add(1)
	senderSendTotal.Add(1)
	return nil
}
