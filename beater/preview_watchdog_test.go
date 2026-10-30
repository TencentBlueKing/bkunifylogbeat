// Tencent is pleased to support the open source community by making bkunifylogbeat 蓝鲸日志采集器 available.
//
// Copyright (C) 2021 THL A29 Limited, a Tencent company.  All rights reserved.
//
// bkunifylogbeat 蓝鲸日志采集器 is licensed under the MIT License.
// See the LICENSE file for details.

package beater

import (
	"sync/atomic"
	"testing"
	"time"

	libgsebeat "github.com/TencentBlueKing/bkmonitor-datalink/pkg/libgse/beat"
	"github.com/elastic/beats/filebeat/input/file"
	"github.com/stretchr/testify/require"

	cfg "github.com/TencentBlueKing/bkunifylogbeat/config"
)

func withNewPreviewWatchdog(t *testing.T, fn func(time.Duration) *time.Timer) {
	t.Helper()
	old := newPreviewWatchdog
	newPreviewWatchdog = fn
	t.Cleanup(func() { newPreviewWatchdog = old })
}

// newWatchdogRunLogBeat builds a preview LogBeat whose task config carries the
// requested direct_read.timeout. No real task is started: the watchdog path is
// driven purely by the manager's preview coordination channels.
func newWatchdogRunLogBeat(t *testing.T, directTimeout string) (*LogBeat, *Manager) {
	t.Helper()
	preparePreviewRegistrar(t)

	config := newPreviewRunConfig()
	task := config.Tasks[0].(map[string]interface{})
	task["direct_read"].(map[string]interface{})["timeout"] = directTimeout

	manager, err := NewManager(config, make(chan struct{}))
	require.NoError(t, err)
	manager.isTestMode = func() bool { return true }
	manager.enableManagedPreview = func() error { return nil }
	manager.startTaskFunc = func(*cfg.TaskConfig, []file.State) error { return nil }

	return &LogBeat{done: make(chan struct{}), manager: manager, config: config}, manager
}

// TestPreviewWatchdogTimeout verifies the absolute watchdog bound: the finite
// reader's direct_read.timeout plus the shared finalization budget, with the
// maximum supported timeout as the fallback when the task value is unusable.
func TestPreviewWatchdogTimeout(t *testing.T) {
	withPreviewTimeouts(t, 10*time.Second, time.Second)

	t.Run("direct_read timeout plus finalization budget", func(t *testing.T) {
		manager := &Manager{
			preview:                     true,
			previewDirectReadTimeout:    5 * time.Second,
			previewDirectReadTimeoutSet: true,
		}
		require.Equal(t, 15*time.Second, manager.PreviewWatchdogTimeout())
	})

	t.Run("missing timeout falls back to the maximum supported", func(t *testing.T) {
		managers := map[string]*Manager{
			"not configured": {preview: true},
			"zero":           {preview: true, previewDirectReadTimeoutSet: true},
			"negative": {
				preview:                     true,
				previewDirectReadTimeout:    -time.Second,
				previewDirectReadTimeoutSet: true,
			},
		}
		for name, manager := range managers {
			require.Equal(t, directReadMaxTimeoutFallback+10*time.Second,
				manager.PreviewWatchdogTimeout(), name)
		}
	})

	t.Run("disabled outside preview mode", func(t *testing.T) {
		manager := &Manager{
			previewDirectReadTimeout:    5 * time.Second,
			previewDirectReadTimeoutSet: true,
		}
		require.Zero(t, manager.PreviewWatchdogTimeout())
	})
}

// TestPreparePreviewCapturesDirectReadTimeout verifies that the watchdog base
// is read from the preview task's own direct_read.timeout instead of a constant.
func TestPreparePreviewCapturesDirectReadTimeout(t *testing.T) {
	withPreviewTimeouts(t, 3*time.Second, time.Second)

	enabled := true
	manager := newPreviewManager(true, nil)
	task := newTaskConfigForPreview(t, "kafka", &enabled)
	require.NoError(t, manager.preparePreview(map[string]*cfg.TaskConfig{task.ID: task}))

	// newTaskConfigForPreview configures direct_read.timeout: 10s.
	require.Equal(t, 13*time.Second, manager.PreviewWatchdogTimeout())
}

// TestPreparePreviewFallsBackWithoutTimeout keeps a malformed preview task
// bounded: Beats rejects a missing timeout when the input starts, and until
// then the watchdog must still use the maximum supported timeout.
func TestPreparePreviewFallsBackWithoutTimeout(t *testing.T) {
	withPreviewTimeouts(t, 2*time.Second, time.Second)

	task, err := cfg.CreateTaskConfig(map[string]interface{}{
		"type":   "kafka",
		"dataid": 999990002,
		"hosts":  []string{"127.0.0.1:19092"},
		"topics": []string{"preview-no-timeout"},
		"direct_read": map[string]interface{}{
			"enabled":    true,
			"max_events": 5,
		},
	})
	require.NoError(t, err)

	manager := newPreviewManager(true, nil)
	require.NoError(t, manager.preparePreview(map[string]*cfg.TaskConfig{task.ID: task}))
	require.Equal(t, directReadMaxTimeoutFallback+2*time.Second, manager.PreviewWatchdogTimeout())
}

// TestLogBeatRunWatchdogAbortsWhenCompletionNeverCloses reproduces the reviewed
// deadlock without a real Kafka reader: output backpressure blocks OnEvent, so
// the finite input never reports completion and Runner.Completion never closes.
// Neither previewInputDone nor previewDone can start the finalization budget;
// the absolute watchdog must still release the publisher, bound Manager.Stop
// and make Run return an error.
func TestLogBeatRunWatchdogAbortsWhenCompletionNeverCloses(t *testing.T) {
	withPreviewTimeouts(t, 40*time.Millisecond, 40*time.Millisecond)
	bt, manager := newWatchdogRunLogBeat(t, "120ms")

	var released atomic.Int32
	withStopPublish(t, func() { released.Add(1) })

	// A Manager.Stop that never returns models the Task still stuck inside
	// Publish; the watchdog must not wait for it without a bound.
	releaseManager := make(chan struct{})
	withStopManager(t, func(*Manager) error {
		<-releaseManager
		return nil
	})
	t.Cleanup(func() { close(releaseManager) })

	start := time.Now()
	err := bt.Run()
	elapsed := time.Since(start)

	require.ErrorContains(t, err, "watchdog")
	require.Equal(t, int32(1), released.Load(),
		"the watchdog must release the publisher exactly once")
	require.True(t, bt.aborted.Load())
	require.True(t, isClosedForTest(bt.done))
	require.False(t, isClosedForTest(manager.PreviewDone()),
		"the test must model a completion signal that never arrives")
	require.GreaterOrEqual(t, elapsed, 120*time.Millisecond+40*time.Millisecond,
		"the watchdog must not fire before direct_read.timeout + finalization budget")
	require.Less(t, elapsed, 2*time.Second,
		"Run must return even while Manager.Stop stays blocked")
}

// TestLogBeatRunWatchdogDoesNotPreemptNormalCompletion verifies the normal
// path: when PreviewDone closes before the watchdog deadline, Run finishes the
// preview instead of taking the forced abort path.
func TestLogBeatRunWatchdogDoesNotPreemptNormalCompletion(t *testing.T) {
	withPreviewTimeouts(t, 50*time.Millisecond, 200*time.Millisecond)
	bt, manager := newWatchdogRunLogBeat(t, "500ms")
	withWaitPublishIdle(t, func(time.Duration) error { return nil })

	var released atomic.Int32
	withStopPublish(t, func() { released.Add(1) })

	manager.startTaskFunc = func(*cfg.TaskConfig, []file.State) error {
		// Completion arrives while the manager is still starting tasks, i.e.
		// well before the watchdog deadline.
		manager.notifyPreviewDone(nil)
		return nil
	}

	start := time.Now()
	err := bt.Run()
	elapsed := time.Since(start)

	require.NoError(t, err)
	require.False(t, bt.aborted.Load(), "a completed preview must not be aborted")
	require.Zero(t, released.Load(), "the normal path must not release the publisher forcibly")
	require.Less(t, elapsed, 400*time.Millisecond)
	require.True(t, isClosedForTest(bt.done))
}

// TestLogBeatRunWatchdogOnlyEnabledInPreviewMode verifies that a formal (non
// preview) run neither arms the watchdog timer nor reports a preview.
func TestLogBeatRunWatchdogOnlyEnabledInPreviewMode(t *testing.T) {
	preparePreviewRegistrar(t)

	oldDone := libgsebeat.Done
	libgsebeat.Done = make(chan bool, 1)
	libgsebeat.Done <- true
	t.Cleanup(func() { libgsebeat.Done = oldDone })

	var armed atomic.Int32
	withNewPreviewWatchdog(t, func(timeout time.Duration) *time.Timer {
		armed.Add(1)
		return time.NewTimer(timeout)
	})

	config := newPreviewRunConfig()
	delete(config.Tasks[0].(map[string]interface{}), "direct_read")

	manager, err := NewManager(config, make(chan struct{}))
	require.NoError(t, err)
	manager.isTestMode = func() bool { return true }
	manager.startTaskFunc = func(*cfg.TaskConfig, []file.State) error { return nil }

	bt := &LogBeat{done: make(chan struct{}), manager: manager, config: config}
	require.NoError(t, bt.Run())
	require.Zero(t, armed.Load(), "the watchdog must not be armed outside preview mode")
	require.Nil(t, manager.PreviewDone())
	require.False(t, bt.aborted.Load())
	require.True(t, isClosedForTest(bt.done))
}
