// Tencent is pleased to support the open source community by making bkunifylogbeat 蓝鲸日志采集器 available.
//
// Copyright (C) 2021 THL A29 Limited, a Tencent company.  All rights reserved.
//
// bkunifylogbeat 蓝鲸日志采集器 is licensed under the MIT License.
// See the LICENSE file for details.

package beater

import (
	"fmt"
	"path/filepath"
	"time"

	libgsebeat "github.com/TencentBlueKing/bkmonitor-datalink/pkg/libgse/beat"
	"github.com/TencentBlueKing/bkmonitor-datalink/pkg/libgse/logp"
	"github.com/elastic/beats/libbeat/common"

	cfg "github.com/TencentBlueKing/bkunifylogbeat/config"
)

var (
	// previewDrainTimeout bounds the wait for ACKs and console flush after the
	// Task pipeline has been drained. It is a failure bound, not a success
	// condition; tests override it.
	previewDrainTimeout = 5 * time.Second
	// previewSignalTimeout bounds how long a shutdown signal waits for the
	// Kafka input to stop and the drain marker to reach the Task.
	previewSignalTimeout = 15 * time.Second

	// waitPublishIdle is a test seam for the final ACK/console drain wait.
	waitPublishIdle = libgsebeat.WaitPublishIdle
)

type directReadPresence struct {
	Present bool
	Enabled bool
}

// inspectDirectRead inspects the task's raw config. The direct_read field is
// passed through to the Beats Kafka input unchanged; bkunifylogbeat only reads
// it to enforce the -T-only combination and to coordinate the preview exit.
func inspectDirectRead(config *cfg.TaskConfig) (directReadPresence, error) {
	if config == nil || config.RawConfig == nil || !config.RawConfig.HasField("direct_read") {
		return directReadPresence{}, nil
	}

	child, err := config.RawConfig.Child("direct_read", -1)
	if err != nil {
		return directReadPresence{}, fmt.Errorf("reading direct_read config: %w", err)
	}
	var raw struct {
		Enabled bool `config:"enabled"`
	}
	if err := child.Unpack(&raw); err != nil {
		return directReadPresence{}, fmt.Errorf("reading direct_read.enabled: %w", err)
	}
	return directReadPresence{Present: true, Enabled: raw.Enabled}, nil
}

func (m *Manager) testMode() bool {
	return m.isTestMode != nil && m.isTestMode()
}

// rawTaskHasEnabledDirectRead reports whether a raw task declares an enabled
// direct-read preview. A malformed direct_read field counts as preview intent
// so the strict loader can report the underlying task error instead of letting
// a broken preview task disappear in the tolerant parse.
func rawTaskHasEnabledDirectRead(raw *common.Config) bool {
	if raw == nil || !raw.HasField("direct_read") {
		return false
	}
	child, err := raw.Child("direct_read", -1)
	if err != nil {
		return true
	}
	var direct struct {
		Enabled bool `config:"enabled"`
	}
	if err := child.Unpack(&direct); err != nil {
		return true
	}
	return direct.Enabled
}

// hasEnabledDirectReadIntent detects the JOB preview path from the raw main and
// secondary configuration before task parsing. Only this path gets strict task
// loading; ordinary -T diagnostics keep the historical per-task tolerance.
func hasEnabledDirectReadIntent(config cfg.Config) bool {
	for _, rawTask := range config.Tasks {
		raw, err := common.NewConfigFrom(rawTask)
		if err != nil {
			continue
		}
		if rawTaskHasEnabledDirectRead(raw) {
			return true
		}
	}

	for _, secConfig := range config.SecConfigs {
		pattern := secConfig.Path + "/" + secConfig.Pattern
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		for _, path := range matches {
			secConfRaw, err := libgsebeat.LoadFile(path)
			if err != nil {
				continue
			}
			localConfigsRaw, err := secConfRaw.Child("local", -1)
			if err != nil {
				continue
			}
			n, err := localConfigsRaw.CountField("")
			if err != nil {
				continue
			}
			for i := 0; i < n; i++ {
				localConfigRaw, err := localConfigsRaw.Child("", i)
				if err != nil {
					continue
				}
				if rawTaskHasEnabledDirectRead(localConfigRaw) {
					return true
				}
			}
		}
	}

	return false
}

// preparePreview validates the -T + direct_read combination before any task
// starts and enables libgse's managed shutdown. It must be called before the
// task goroutines are allowed to publish their first event.
func (m *Manager) preparePreview(tasks map[string]*cfg.TaskConfig) error {
	m.preview = false
	m.previewTaskID = ""
	m.previewDone = nil
	m.previewErr = nil

	var directTaskIDs []string
	for taskID, taskConfig := range tasks {
		presence, err := inspectDirectRead(taskConfig)
		if err != nil {
			return fmt.Errorf("task %s: %w", taskID, err)
		}
		if !presence.Present {
			continue
		}
		if !m.testMode() {
			return fmt.Errorf("task %s: direct_read is only allowed with -T", taskID)
		}
		if taskConfig.Type != "kafka" {
			return fmt.Errorf("task %s: direct_read requires a kafka task, got %q", taskID, taskConfig.Type)
		}
		if !presence.Enabled {
			continue
		}
		directTaskIDs = append(directTaskIDs, taskID)
	}

	if len(directTaskIDs) == 0 {
		return nil
	}
	if len(tasks) != 1 || len(directTaskIDs) != 1 {
		return fmt.Errorf("direct_read preview requires exactly one task, got %d tasks and %d direct tasks",
			len(tasks), len(directTaskIDs))
	}
	if m.enableManagedPreview == nil {
		return fmt.Errorf("managed preview is not available")
	}
	if err := m.enableManagedPreview(); err != nil {
		return fmt.Errorf("enabling managed preview: %w", err)
	}

	m.preview = true
	m.previewTaskID = directTaskIDs[0]
	m.previewDone = make(chan struct{})
	logp.L.Infof("preview mode enabled for task %s", m.previewTaskID)
	return nil
}

// validateDirectTask covers the reload path too: a direct_read task must never
// start outside the single managed preview task.
func (m *Manager) validateDirectTask(config *cfg.TaskConfig) error {
	presence, err := inspectDirectRead(config)
	if err != nil {
		return err
	}
	if !presence.Present {
		return nil
	}
	if !m.testMode() {
		return fmt.Errorf("task %s: direct_read is only allowed with -T", config.ID)
	}
	if config.Type != "kafka" {
		return fmt.Errorf("task %s: direct_read requires a kafka task, got %q", config.ID, config.Type)
	}
	if !presence.Enabled && !m.preview {
		return nil // Beats validates any incompatible direct_read limits.
	}
	if !m.preview {
		return fmt.Errorf("task %s: direct_read requires an active managed preview", config.ID)
	}
	if !presence.Enabled || config.ID != m.previewTaskID {
		return fmt.Errorf("task %s: direct_read task does not match the active preview task", config.ID)
	}
	return nil
}

func (m *Manager) notifyPreviewDone(err error) {
	m.previewOnce.Do(func() {
		m.previewMu.Lock()
		m.previewErr = err
		m.previewMu.Unlock()
		if m.previewDone != nil {
			close(m.previewDone)
		}
	})
}

// PreviewDone returns the preview completion signal, or nil when the current
// process is not running a managed direct-read preview.
func (m *Manager) PreviewDone() <-chan struct{} {
	if !m.preview {
		return nil
	}
	return m.previewDone
}

// PreviewErr returns the terminal Kafka/drain error, or nil on success.
func (m *Manager) PreviewErr() error {
	m.previewMu.Lock()
	defer m.previewMu.Unlock()
	return m.previewErr
}

// StopPreviewInputs stops the finite Kafka producer without tearing down the
// downstream Task nodes, so the pipeline can still drain on the signal path.
func (m *Manager) StopPreviewInputs() {
	if !m.preview {
		return
	}
	if previewTask, ok := m.tasks[m.previewTaskID]; ok {
		logp.L.Infof("stopping preview input for task %s", m.previewTaskID)
		previewTask.StopInput()
	}
}
