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

package registrar

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	cfg "github.com/TencentBlueKing/bkunifylogbeat/config"
	"github.com/TencentBlueKing/bkunifylogbeat/task/input/wineventlog"
	"github.com/TencentBlueKing/collector-go-sdk/v2/bkbeat/bkmonitoring"
	"github.com/TencentBlueKing/collector-go-sdk/v2/bkbeat/logp"
	bkStorage "github.com/TencentBlueKing/collector-go-sdk/v2/bkbeat/storage"
	"github.com/elastic/beats/filebeat/input/file"
	commonFile "github.com/elastic/beats/libbeat/common/file"
	"github.com/elastic/beats/libbeat/monitoring"
)

const (
	registrarKey    = "registrar"
	timeKey         = "localtime"
	stateNanosecond = 1
	stateNotManage  = -2
)

var (
	registrarFlushed      = bkmonitoring.NewInt("registrar_flushed")
	registrarMarshalError = bkmonitoring.NewInt("registrar_marshal_error")
	registrarFiles        = bkmonitoring.NewInt("registrar_files", monitoring.Gauge)
)

// Registrar: 采集进度管理
type Registrar struct {
	Channel chan []file.State
	done    chan struct{}
	wg      sync.WaitGroup

	states       *file.States // Map with all file paths inside and the corresponding state
	gcRequired   bool         // gcRequired is set if registry state needs to be gc'ed before the next write
	flushTimeout time.Duration
	gcFrequency  time.Duration
}

// New creates a new Registrar instance, updating the registry file on
// `file.State` updates. New fails if the file can not be opened or created.
func New(config cfg.Registry) (*Registrar, error) {
	r := &Registrar{
		done: make(chan struct{}),
		wg:   sync.WaitGroup{},

		states:       file.NewStates(),
		Channel:      make(chan []file.State, 1),
		flushTimeout: config.FlushTimeout,
		gcFrequency:  config.GcFrequency,
	}
	return r, r.Init()
}

// Init: 采集器启动时调用，同时对原采集器采集进度迁移
func (r *Registrar) Init() error {
	var states []file.State

	// get time
	str, err := bkStorage.Get(timeKey)
	if err != nil {
		if err == bkStorage.ErrNotFound {
			return nil
		} else {
			return fmt.Errorf("get %s from bkStorage error", timeKey)
		}
	}
	t, err := time.Parse(time.UnixDate, str)
	if err != nil {
		return fmt.Errorf("parse time error: %v", err)
	}

	// get registrar
	str, err = bkStorage.Get(registrarKey)
	if err != nil {
		if err == bkStorage.ErrNotFound {
			return nil
		} else {
			return fmt.Errorf("get %s from bkStorage err: %v", registrarKey, err)
		}
	}

	err = json.Unmarshal([]byte(str), &states)
	if err != nil {
		logp.L.Errorf("json unmarshal error, %s", str)
		return fmt.Errorf("error decoding states: %s", err)
	}
	states = r.migrate(states)
	logp.L.Infof("load states: time=>%s, count=>%d, flush=>%s, gcFrequency=>%s",
		t, len(states), r.flushTimeout, r.gcFrequency)

	r.states.SetStates(ResetStates(states))
	return nil
}

// GetStates return the registrar states
func (r *Registrar) GetStates() []file.State {
	return r.states.GetStates()
}

// resetStates sets all states to finished and disable TTL on restart
// For all states covered by an input, TTL will be overwritten with the input value
func ResetStates(states []file.State) []file.State {
	for key, state := range states {
		state.Finished = true
		// Set ttl to -2 to easily spot which states are not managed by a input
		state.TTL = stateNotManage
		states[key] = state
	}
	return states
}

// Start start the registry.
func (r *Registrar) Start() error {
	r.wg.Add(1)
	go r.run()

	return nil
}

// Stop stops the registry. It waits until Run function finished.
func (r *Registrar) Stop() {
	logp.L.Info("Stopping Registrar")
	close(r.done)
	r.wg.Wait()
}

func (r *Registrar) run() {
	logp.L.Debug("registrar", "Starting Registrar")
	// Writes registry on shutdown
	flushTicker := time.NewTicker(r.flushTimeout)
	gcTicker := time.NewTicker(r.gcFrequency)

	defer func() {
		flushTicker.Stop()
		gcTicker.Stop()
		r.flushRegistry()
		r.wg.Done()
	}()

	for {
		select {
		case <-r.done:
			logp.L.Info("Ending Registrar")
			return
		case <-flushTicker.C:
			r.flushRegistry()
		case <-gcTicker.C:
			r.gcRequired = true
		case states := <-r.Channel:
			r.onEvents(states)
		}
	}
}

// onEvents processes events received from the publisher pipeline
func (r *Registrar) onEvents(states []file.State) {
	logp.L.Debugf("registrar state updates processed. Count: %d", len(states))

	ts := time.Now()
	for _, s := range states {
		if s.Type == wineventlog.WinLogFileStateType {
			r.states.UpdateWithTs(s, s.Timestamp)
		} else {
			r.states.UpdateWithTs(s, ts)
		}
	}
}

// flushRegistry writes the registry to disk.
func (r *Registrar) flushRegistry() {
	registrarFlushed.Add(1)

	// First clean up states
	r.gcStates()
	states := r.GetStates()
	bytes, err := json.Marshal(states)
	if err != nil {
		registrarMarshalError.Add(1)
		logp.L.Errorf("Writing of registry returned error: %v. Continuing...", err)
		return
	}

	//采集文件数量
	registrarFiles.Set(int64(len(states)))

	bkStorage.Set(registrarKey, string(bytes), 0)
	bkStorage.Set(timeKey, time.Now().Format(time.UnixDate), 0)
}

// migrate file state
func (r *Registrar) migrate(states []file.State) []file.State {
	if len(states) == 0 || states[0].Type != "" {
		return states
	}
	stateEmpty := commonFile.StateOS{}
	for key, state := range states {
		if state.FileStateOS == stateEmpty && state.Type == "" && state.Source != "" {
			fileInfo, err := os.Stat(state.Source)
			if err != nil {
				logp.L.Debugf("input", "stat(%s) failed: %s", state.Source, err)
				continue
			}
			state.Type = "log"
			state.Fileinfo = fileInfo
			state.FileStateOS = commonFile.GetOSState(state.Fileinfo)
			states[key] = state
		}
	}
	logp.L.Infof("migrate file states: %d", len(states))
	return states
}

// gcStates runs a registry Cleanup. The method check if more event in the
// registry can be gc'ed in the future. If no potential removable state is found,
// the gcEnabled flag is set to false, indicating the current registrar state being
// stable. New registry update events can re-enable state gc'ing.
func (r *Registrar) gcStates() {
	if !r.gcRequired {
		return
	}

	// 清理所有未在Input管理的states
	states := r.states.GetStates()
	if len(states) == 0 {
		return
	}

	for _, state := range states {
		if state.TTL == stateNotManage {
			state.TTL = stateNanosecond
			r.states.Update(state)
		}
	}

	// 直接清理已过期的文件
	beforeCount := r.states.Count()
	cleanedStates, pendingClean := r.states.Cleanup()

	logp.L.Infof("Registrar states cleaned up. Before: %d, After: %d, Pending: %d",
		beforeCount, beforeCount-cleanedStates, pendingClean)

	r.gcRequired = false
}
