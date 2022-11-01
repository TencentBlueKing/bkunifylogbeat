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
	"sync"
	"time"

	"github.com/TencentBlueKing/bkunifylogbeat/config"
	"github.com/TencentBlueKing/bkunifylogbeat/task/base"
	"github.com/TencentBlueKing/bkunifylogbeat/task/formatter"
	"github.com/TencentBlueKing/collector-go-sdk/v2/bkbeat/beat"
	"github.com/TencentBlueKing/collector-go-sdk/v2/bkbeat/bkmonitoring"
	"github.com/TencentBlueKing/collector-go-sdk/v2/bkbeat/logp"
	"github.com/elastic/beats/filebeat/util"
)

var (
	senderReceived  = bkmonitoring.NewInt("sender_received")   // 兼容指标
	senderState     = bkmonitoring.NewInt("sender_state")      // 兼容指标
	senderSendTotal = bkmonitoring.NewInt("sender_send_total") // 兼容指标

	senderMaps = map[string]*Sender{}
	mtx        sync.RWMutex

	numOfSenderTotal = bkmonitoring.NewInt("task_sender_total") // 当前全局sender的数量

	senderDroppedTotal = bkmonitoring.NewInt("send_dropped_total")
	senderHandledTotal = bkmonitoring.NewInt("send_handled_total")
)

// Sender : 对采集事件进行打包, 并调用beat发送事件
type Sender struct {
	*base.Node

	sendConfig config.SenderConfig

	cache      map[string][]*util.Data
	cacheInput chan *util.Data

	formatter      formatter.Formatter
	taskConfigMaps map[string]*config.TaskConfig
}

func GetSender(taskCfg *config.TaskConfig, taskNode *base.TaskNode) (*Sender, error) {
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
		send.AddOutput(taskNode.Node)
		send.AddTaskNode(taskNode.Node, taskNode)
		return send, nil
	}
	return NewSender(taskCfg, taskNode)
}

func NewSender(taskCfg *config.TaskConfig, taskNode *base.TaskNode) (*Sender, error) {
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

	send.AddOutput(taskNode.Node)
	send.AddTaskNode(taskNode.Node, taskNode)

	go send.Run()

	logp.L.Infof("add sender(%s) in global senderMaps", send.ID)
	mtx.Lock()
	defer mtx.Unlock()
	senderMaps[taskCfg.SenderID] = send
	numOfSenderTotal.Add(1)
	return send, nil
}

// RemoveSender : 移除全局缓存
func RemoveSender(id string) {
	logp.L.Infof("remove sender(%s) in global senderMaps", id)
	mtx.Lock()
	defer mtx.Unlock()
	delete(senderMaps, id)
	numOfSenderTotal.Add(-1)
}

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

	return nil
}

func (send *Sender) Run() {
	defer close(send.GameOver)
	defer RemoveSender(send.ID)

	senderTicker := time.NewTicker(1 * time.Second)
	defer senderTicker.Stop()
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
			// update metric
			{
				base.CrawlerSendTotal.Add(1)
				senderReceived.Add(1)
				for _, taskNodeList := range send.TaskNodeList {
					for _, tNode := range taskNodeList {
						tNode.CrawlerSendTotal.Add(1)
						tNode.SenderReceive.Add(1)
					}
				}
			}
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
			senderDroppedTotal.Add(1)
			logp.L.Errorf("send to out error, out's taskConfig is nil, %s", taskID)
			continue
		}

		if formattedEvent == nil {
			packageEvent = beat.Event{
				Fields:  nil,
				Private: lastState,
			}

			senderState.Add(1)
			for _, taskNodeList := range send.TaskNodeList {
				for _, tNode := range taskNodeList {
					tNode.SenderState.Add(1)
				}
			}
		} else {
			data := formattedEvent.Clone()
			data["dataid"] = taskConfig.DataID

			//处理状态事件
			packageEvent = beat.Event{
				Fields:  data,
				Private: lastState,
			}
			// 发送到pipeline的数量
			senderSendTotal.Add(1)
			for _, taskNodeList := range send.TaskNodeList {
				for _, tNode := range taskNodeList {
					tNode.SenderSendTotal.Add(1)
				}
			}
		}
		select {
		case <-send.End:
			logp.L.Infof("node send(%s) is done", send.ID)
			return
		case out <- packageEvent:
			senderHandledTotal.Add(1)
		}
	}
}
