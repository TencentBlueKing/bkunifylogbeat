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
	"fmt"
	"github.com/elastic/beats/filebeat/channel"
	"github.com/elastic/beats/filebeat/harvester"
	"github.com/elastic/beats/filebeat/input"
	"github.com/elastic/beats/libbeat/beat"
	"github.com/elastic/beats/libbeat/common"
	"github.com/elastic/beats/libbeat/logp"
	"github.com/elastic/beats/winlogbeat/checkpoint"
	"github.com/elastic/beats/winlogbeat/eventlog"
	"sync"
)

func init() {
	err := input.Register("winlog", NewInput)
	if err != nil {
		panic(err)
	}
}

// Input defines a udp input to receive event on a specific host:port.
type Input struct {
	started bool
	mutex   sync.Mutex
	outlet  channel.Outleter

	eventLogs []*eventLogger

	config   config
	cfg      *common.Config
	registry *harvester.Registry

	done     chan struct{}                       // Channel to initiate shutdown of main event loop.
	pipeline beat.Pipeline                       // Interface to publish event.
	states   map[string]checkpoint.EventLogState // win event log states
	wg       sync.WaitGroup
}

// Reload runs the input
func (p *Input) Reload() {
	return
}

func (p *Input) processEventLog(
	logger *eventLogger,
	state checkpoint.EventLogState,
) {
	defer p.wg.Done()
	logger.run(p.done, p.outlet, state)
}

// Run start a windows event log input
func (p *Input) Run() {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if !p.started {
		for _, log := range p.eventLogs {
			state, _ := p.states[log.source.Name()]

			// Start a goroutine for each event log.
			p.wg.Add(1)
			go p.processEventLog(log, state)
		}

		p.started = true

	}
}

// Stop stops windows event log
func (p *Input) Stop() {
	logp.Info("Stopping Winlog Collect")
	if p.done != nil {
		close(p.done)
	}
	p.wg.Wait()

	p.registry.Stop()
	_ = p.outlet.Close()
}

// Wait stop the current server
func (p *Input) Wait() {
	p.Stop()
}

// NewInput: creates a new windows event input
func NewInput(
	cfg *common.Config,
	outletFactory channel.Connector,
	context input.Context,
) (input.Input, error) {
	config := defaultConfig
	err := cfg.Unpack(&config)
	if err != nil {
		return nil, err
	}

	if len(config.EventLogs) <= 0 {
		return nil, fmt.Errorf("at least one event log must be configured as part of event_logs")
	}

	outlet, err := outletFactory(cfg, context.DynamicFields)
	if err != nil {
		return nil, err
	}

	eventLogs := make([]*eventLogger, 0, len(config.EventLogs))
	for _, config := range config.EventLogs {
		eventLog, err := eventlog.New(config)
		if err != nil {
			logp.Err("Failed to create new event log. %v", err)
			continue
		}
		logp.Info("Initialized EventLog[%s]", eventLog.Name())

		logger, err := newEventLogger(eventLog, config)
		if err != nil {
			logp.Err("Failed to create new event log. %v", err)
			continue
		}

		eventLogs = append(eventLogs, logger)
	}

	winStates := make(map[string]checkpoint.EventLogState)
	for _, s := range context.States {
		if s.Type == WinLogFileStateType {
			winStates[s.Source] = FileStateToWinLogState(s)
		}
	}

	p := &Input{
		started: false,
		outlet:  outlet,

		eventLogs: eventLogs,

		config:   config,
		cfg:      cfg,
		registry: harvester.NewRegistry(),
		states:   winStates,
	}

	return p, nil
}
