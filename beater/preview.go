// Tencent is pleased to support the open source community by making bkunifylogbeat 蓝鲸日志采集器 available.
//
// Copyright (C) 2021 THL A29 Limited, a Tencent company.  All rights reserved.
//
// bkunifylogbeat 蓝鲸日志采集器 is licensed under the MIT License.
// See the LICENSE file for details.

package beater

import (
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	libgsebeat "github.com/TencentBlueKing/bkmonitor-datalink/pkg/libgse/beat"
	"github.com/TencentBlueKing/bkmonitor-datalink/pkg/libgse/logp"
	"github.com/elastic/beats/libbeat/common"

	cfg "github.com/TencentBlueKing/bkunifylogbeat/config"
)

var (
	// previewFinalizeTimeout is the fixed finalization budget. It starts at
	// whichever happens first: the finite input reporting completion, or a
	// shutdown signal. The same window covers waiting for the drain marker and
	// waiting for ACKs/console flush, so a late signal cannot extend shutdown.
	previewFinalizeTimeout = 10 * time.Second
	// previewStopTimeout bounds Manager.Stop after the drain completed or the
	// finalization budget expired. Manager.Stop joins the Task goroutines and
	// can still block when an output worker is stuck, but Run must always return
	// so Registrar.Stop and beat.Stop can release the Storage.
	previewStopTimeout = 5 * time.Second

	// waitPublishIdle is a test seam for the final ACK/console drain wait.
	waitPublishIdle = libgsebeat.WaitPublishIdle
	// stopPublish is a test seam for the forced publisher disconnect that
	// releases a Task or Sender goroutine blocked inside Publish.
	stopPublish = libgsebeat.StopPublish
	// stopManager is a test seam for the Manager.Stop join launched by
	// startManagerStop. Production always stops the real Manager.
	stopManager = func(m *Manager) error { return m.Stop() }
	// newPreviewWatchdog is a test seam for the absolute preview watchdog
	// timer. Production arms a real timer; tests can shorten or observe it.
	newPreviewWatchdog = func(timeout time.Duration) *time.Timer { return time.NewTimer(timeout) }
)

// directReadMaxTimeoutFallback mirrors the maximum direct_read.timeout accepted
// by the Beats Kafka input. It is only used when a preview task cannot be
// parsed into a usable timeout, so a malformed preview task is still bounded by
// the largest supported reader lifetime instead of silently disabling the
// watchdog.
const directReadMaxTimeoutFallback = 60 * time.Second

// errPreviewFinalizeTimeout is the terminal reason when the shared shutdown
// budget expires before the drain marker and the ACK drain are complete.
var errPreviewFinalizeTimeout = errors.New("preview finalization budget expired")

// errPreviewWatchdogExpired is the terminal reason when the absolute preview
// watchdog fires. It means neither the finite input completion nor the preview
// completion arrived before direct_read.timeout plus the finalization budget,
// typically because a Task is blocked inside OnEvent by output backpressure.
var errPreviewWatchdogExpired = errors.New("preview watchdog expired before completion")

// previewFinalizeBudget is the single fixed window shared by the normal
// completion path and the shutdown-signal path.
type previewFinalizeBudget struct {
	timeout  time.Duration
	deadline time.Time
	timer    *time.Timer
	started  bool
}

func newPreviewFinalizeBudget(timeout time.Duration) *previewFinalizeBudget {
	return &previewFinalizeBudget{timeout: timeout}
}

// start arms the budget on first use and returns its expiry channel. Repeated
// calls return the same channel and never extend the deadline.
func (b *previewFinalizeBudget) start() <-chan time.Time {
	if b.timer == nil {
		b.started = true
		b.deadline = time.Now().Add(b.timeout)
		b.timer = time.NewTimer(b.timeout)
	}
	return b.timer.C
}

// remaining reports the part of the budget that is still available. Before the
// budget is armed it reports the full timeout.
func (b *previewFinalizeBudget) remaining() time.Duration {
	if !b.started {
		return b.timeout
	}
	return time.Until(b.deadline)
}

func (b *previewFinalizeBudget) expired() bool {
	return b.started && !time.Now().Before(b.deadline)
}

func (b *previewFinalizeBudget) stop() {
	if b.timer != nil {
		b.timer.Stop()
	}
}

type directReadPresence struct {
	Present bool
	Enabled bool
	// Timeout is the configured direct_read.timeout when HasTimeout is true.
	// Beats requires it for an enabled direct_read block; it bounds the finite
	// reader and is the base of the absolute preview watchdog.
	Timeout    time.Duration
	HasTimeout bool
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
		Enabled bool           `config:"enabled"`
		Timeout *time.Duration `config:"timeout"`
	}
	if err := child.Unpack(&raw); err != nil {
		return directReadPresence{}, fmt.Errorf("reading direct_read config: %w", err)
	}

	presence := directReadPresence{Present: true, Enabled: raw.Enabled}
	if raw.Timeout != nil {
		presence.Timeout = *raw.Timeout
		presence.HasTimeout = true
	}
	return presence, nil
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
	m.previewInputDone = nil
	m.previewErr = nil
	m.previewDirectReadTimeout = 0
	m.previewDirectReadTimeoutSet = false
	// The notification guards belong to one preview generation: preparePreview
	// replaces them together with the channels so a re-entered preview can
	// signal its new channels again. Pointers are used because copying a
	// sync.Once value is rejected by vet's copylocks check.
	m.previewOnce = &sync.Once{}
	m.previewInputOnce = &sync.Once{}

	var directTaskIDs []string
	var directReadTimeout time.Duration
	var directReadTimeoutSet bool
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
		directReadTimeout = presence.Timeout
		directReadTimeoutSet = presence.HasTimeout
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
	m.previewDirectReadTimeout = directReadTimeout
	m.previewDirectReadTimeoutSet = directReadTimeoutSet
	m.previewDone = make(chan struct{})
	m.previewInputDone = make(chan struct{})
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
	once := m.previewOnce
	if once == nil {
		return
	}
	once.Do(func() {
		// The marker cannot arrive before the finite input completed. Making the
		// input signal idempotently fire here as well guarantees the coordinator
		// has a budget even if the completion notification raced with teardown.
		m.notifyPreviewInputDone()

		m.previewMu.Lock()
		m.previewErr = err
		m.previewMu.Unlock()
		if m.previewDone != nil {
			close(m.previewDone)
		}
	})
}

// notifyPreviewInputDone signals that the finite preview input stopped
// producing and the drain marker is about to be injected. The coordinator
// starts its fixed shutdown budget here, before the marker can be blocked by an
// output that stopped consuming.
func (m *Manager) notifyPreviewInputDone() {
	if m.previewInputDone == nil {
		return
	}
	once := m.previewInputOnce
	if once == nil {
		return
	}
	once.Do(func() {
		close(m.previewInputDone)
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

// PreviewInputDone returns the finite-input completion signal, or nil when the
// current process is not running a managed direct-read preview. The signal
// fires before the drain marker is injected.
func (m *Manager) PreviewInputDone() <-chan struct{} {
	if !m.preview {
		return nil
	}
	return m.previewInputDone
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

// PreviewWatchdogTimeout returns the absolute lifetime of the managed preview:
// the finite reader's own direct_read.timeout plus the shared finalization
// budget. It deliberately does not depend on Runner.Completion, so a Task
// blocked inside OnEvent (output backpressure) still cannot keep the process
// alive past this bound.
//
// The fallback covers a preview task whose direct_read.timeout could not be
// parsed (Beats rejects that configuration when the input starts): the preview
// is then bounded by the maximum supported reader timeout plus the finalization
// budget instead of disabling the watchdog.
func (m *Manager) PreviewWatchdogTimeout() time.Duration {
	if !m.preview {
		return 0
	}

	timeout := m.previewDirectReadTimeout
	if !m.previewDirectReadTimeoutSet || timeout <= 0 {
		logp.L.Warnf("preview task has no usable direct_read.timeout; watchdog uses the maximum supported timeout %s",
			directReadMaxTimeoutFallback)
		timeout = directReadMaxTimeoutFallback
	}
	return timeout + previewFinalizeTimeout
}
