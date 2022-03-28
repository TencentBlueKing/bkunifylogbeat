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

package sender

import (
	"fmt"
	"github.com/TencentBlueKing/bkunifylogbeat/task/base"
	"github.com/TencentBlueKing/bkunifylogbeat/task/formatter"
	"sync"
	"time"

	"github.com/TencentBlueKing/bkunifylogbeat/config"
	"github.com/TencentBlueKing/collector-go-sdk/v2/bkbeat/beat"
	"github.com/TencentBlueKing/collector-go-sdk/v2/bkbeat/logp"
	"github.com/elastic/beats/filebeat/util"
)

var (
	//senderReceived  = bkmonitoring.NewInt("sender_received")
	//senderState     = bkmonitoring.NewInt("sender_state")
	//senderSendTotal = bkmonitoring.NewInt("sender_send_total")

	senderMaps = map[string]*Sender{}
	mtx        sync.RWMutex
)

// Sender : 对采集事件进行打包, 并调用beat发送事件
type Sender struct {
	*base.Node

	sendConfig config.SenderConfig

	cache      map[string][]*util.Data
	cacheInput chan *util.Data

	formatter      formatter.Formatter
	taskConfigMaps map[string]*config.TaskConfig

	//senderReceive   *monitoring.Int // 接收的事件数
	//senderSendTotal *monitoring.Int // 发送到pipeline的数量
	//senderState     *monitoring.Int // 仅需要更新采集状态的事件数(event.Field为空)
}

func GetSender(taskCfg *config.TaskConfig, leafNode *base.Node) (*Sender, error) {
	var (
		ok   bool
		send *Sender
	)

	func() {
		mtx.RLock()
		defer mtx.RUnlock()
		send, ok = senderMaps[taskCfg.SenderID]
	}()

	if ok {
		err := send.MergeSenderConfig(taskCfg)
		if err != nil {
			return nil, err
		}
		send.AddOutput(leafNode)
		return send, nil
	}
	return NewSender(taskCfg, leafNode)
}

func NewSender(taskCfg *config.TaskConfig, leafNode *base.Node) (*Sender, error) {
	var err error
	var send = &Sender{
		Node: base.NewEmptyNode(taskCfg.SenderID),

		cache:      make(map[string][]*util.Data),
		cacheInput: make(chan *util.Data),

		taskConfigMaps: map[string]*config.TaskConfig{},
	}
	err = send.MergeSenderConfig(taskCfg)
	if err != nil {
		return nil, err
	}

	send.AddOutput(leafNode)

	go send.Run()

	logp.L.Infof("add sender(%s) in global senderMaps", send.ID)
	mtx.Lock()
	defer mtx.Unlock()
	senderMaps[taskCfg.SenderID] = send
	return send, err
}

// RemoveSender : 移除全局缓存
func RemoveSender(id string) {
	logp.L.Infof("remove sender(%s) in global senderMaps", id)
	mtx.Lock()
	defer mtx.Unlock()
	delete(senderMaps, id)
}

// PublisherFunc : 接收采集事件并发送到outlet
type PublisherFunc func(beat.Event) bool

// MergeSenderConfig 生成采集器Sender实例
// 理论上Merge这里不存在任何动作，因为Sender的配置是一样的
func (send *Sender) MergeSenderConfig(taskCfg *config.TaskConfig) error {
	send.taskConfigMaps[taskCfg.ID] = taskCfg

	// 因为配置一致，所以这里判断如果已经生成了则直接返回
	if send.formatter != nil {
		return nil
	}

	//formatter
	outputFormat := taskCfg.OutputFormat
	if outputFormat == "" {
		outputFormat = "default"
	}
	f, err := formatter.FindFormatterFactory(outputFormat)
	if err != nil {
		return err
	}
	send.formatter, err = f(taskCfg)
	if err != nil {
		return err
	}
	send.sendConfig = taskCfg.SenderConfig

	//sender metrics
	//send.senderReceive = bkmonitoring.NewIntWithDataID(config.DataID, "sender_received")
	//send.senderState = bkmonitoring.NewIntWithDataID(config.DataID, "sender_state")
	//send.senderSendTotal = bkmonitoring.NewIntWithDataID(config.DataID, "sender_send_total")

	return nil
}

func (send *Sender) Run() {
	defer RemoveSender(send.ID)

	senderTicker := time.NewTicker(1 * time.Second)
	senderTicker.Stop()
	for {
		select {
		case <-send.End:
			logp.L.Infof("sender quit, id: %s", send.String())
			return

		case <-senderTicker.C:
			// clear cache
			for _, buffer := range send.cache {
				if len(buffer) > 0 {
					send.send(buffer)
				}
			}
			send.cache = make(map[string][]*util.Data)

		case e := <-send.In:
			event := e.(*util.Data)
			err := send.cacheSend(event)
			if err != nil {
				logp.L.Errorf("send event error, %v", err)
				continue
			}
		}
	}
}

// Sender 实例名称
func (send *Sender) String() string {
	return fmt.Sprintf("Sender-SenderID-%s", send.ID)
}

func (send *Sender) cacheSend(event *util.Data) error {
	source := event.GetState().Source

	if !send.sendConfig.CanPackage {
		send.send([]*util.Data{event})
		return nil
	}

	buffer, exist := send.cache[source]
	// 特殊事件直接发送
	if event.Event.Fields == nil {
		if exist {
			buffer = append(buffer, event)
			send.send(buffer)
			send.cache[source] = []*util.Data{}
			return nil
		}
		send.send([]*util.Data{event})
		return nil
	}

	//正常事件处理
	if exist {
		send.cache[source] = append(buffer, event)
	} else {
		send.cache[source] = []*util.Data{event}
	}

	// if msg count reach max count, clear cache
	if len(send.cache[source]) >= send.sendConfig.PackageCount {
		send.send(send.cache[source])
		// clear cache
		send.cache[source] = []*util.Data{}
	}
	return nil
}

// send: 调用beat.SendEvent发送打包后的采集事件
func (send *Sender) send(events []*util.Data) {
	var packageEvent beat.Event
	if len(events) == 0 {
		return
	}

	lastState := events[len(events)-1].GetState()
	formattedEvent := send.formatter.Format(events)

	// send data
	for taskID, out := range send.Outs {
		taskConfig, ok := send.taskConfigMaps[taskID]
		if !ok {
			logp.L.Errorf("send to out error, out's taskConfig is nil, %s", taskID)
			continue
		}

		data := formattedEvent.Clone()
		data["dataid"] = taskConfig.DataID
		//处理状态事件
		if data == nil {
			//send.senderState.Add(1)
			//senderState.Add(1)

			packageEvent = beat.Event{
				Fields:  nil,
				Private: lastState,
			}
		} else {
			packageEvent = beat.Event{
				Fields:  data,
				Private: lastState,
			}
			// 发送到pipeline的数量
			//send.senderSendTotal.Add(1)
			//senderSendTotal.Add(1)
		}
		select {
		case <-send.End:
			logp.L.Infof("node filter(%s) is done", send.ID)
			return
		case out <- packageEvent:
		}
	}
}
