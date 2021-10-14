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

package wineventlog

import (
	"github.com/elastic/beats/filebeat/channel"
	"github.com/elastic/beats/filebeat/util"
	"time"

	"github.com/elastic/beats/libbeat/beat"
	"github.com/elastic/beats/libbeat/common"
	"github.com/elastic/beats/libbeat/logp"
	"github.com/elastic/beats/libbeat/processors"

	"github.com/elastic/beats/winlogbeat/checkpoint"
	"github.com/elastic/beats/winlogbeat/eventlog"
)

type eventLogger struct {
	source     eventlog.EventLog
	eventMeta  common.EventMetadata
	processors beat.ProcessorList
}

type eventLoggerConfig struct {
	common.EventMetadata `config:",inline"`      // Fields and tags to add to events.
	Processors           processors.PluginConfig `config:"processors"`
}

func newEventLogger(
	source eventlog.EventLog,
	options *common.Config,
) (*eventLogger, error) {
	config := eventLoggerConfig{}
	if err := options.Unpack(&config); err != nil {
		return nil, err
	}

	processors, err := processors.New(config.Processors)
	if err != nil {
		return nil, err
	}

	return &eventLogger{
		source:     source,
		eventMeta:  config.EventMetadata,
		processors: processors,
	}, nil
}

func (e *eventLogger) connect(pipeline beat.Pipeline) (beat.Client, error) {
	api := e.source.Name()
	return pipeline.ConnectWith(beat.ClientConfig{
		PublishMode: beat.GuaranteedSend,
		Processing: beat.ProcessingConfig{
			EventMetadata: e.eventMeta,
			Meta:          nil, // TODO: configure modules/ES ingest pipeline?
			Processor:     e.processors,
		},
		ACKCount: func(n int) {
			//addPublished(api, n)  // TODO 增加指标监控
			logp.Info("EventLog[%s] successfully published %d events", api, n)
		},
	})
}

func (e *eventLogger) run(
	done <-chan struct{},
	outlet channel.Outleter,
	state checkpoint.EventLogState,
) {
	api := e.source

	// Initialize per event log metrics.
	//initMetrics(api.Name())  // TODO 增加指标监控

	//client, err := e.connect(pipeline)
	//if err != nil {
	//	logp.Warn("EventLog[%s] Pipeline error. Failed to connect to publisher pipeline",
	//		api.Name())
	//	return
	//}
	//
	//// close client on function return or when `done` is triggered (unblock client)
	//defer client.Close()
	//go func() {
	//	<-done
	//	client.Close()
	//}()

	err := api.Open(state)
	if err != nil {
		logp.Warn("EventLog[%s] Open() error. No events will be read from "+
			"this source. %v", api.Name(), err)
		return
	}
	defer func() {
		logp.Info("EventLog[%s] Stop processing.", api.Name())

		if err := api.Close(); err != nil {
			logp.Warn("EventLog[%s] Close() error. %v", api.Name(), err)
			return
		}
	}()

	logp.Debug("wineventlog", "EventLog[%s] opened successfully", api.Name())

	for {
		select {
		case <-done:
			return
		default:
		}

		// Read from the event.
		records, err := api.Read()
		if err != nil {
			logp.Warn("EventLog[%s] Read() error: %v", api.Name(), err)
			break
		}

		logp.Debug("wineventlog", "EventLog[%s] Read() returned %d records", api.Name(), len(records))
		if len(records) == 0 {
			// TODO: Consider implementing notifications using
			// NotifyChangeEventLog instead of polling.
			time.Sleep(time.Second)
			continue
		}

		for _, lr := range records {
			data := util.NewData()
			data.Event = ToEvent(lr)
			outlet.OnEvent(data)
		}
	}
}
