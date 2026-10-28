// Tencent is pleased to support the open source community by making bkunifylogbeat 蓝鲸日志采集器 available.
//
// Copyright (C) 2021 THL A29 Limited, a Tencent company.  All rights reserved.
//
// bkunifylogbeat 蓝鲸日志采集器 is licensed under the MIT License.
// See the LICENSE file for details.

package beater

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	libgsebeat "github.com/TencentBlueKing/bkmonitor-datalink/pkg/libgse/beat"
	"github.com/elastic/beats/filebeat/input/file"
	"github.com/stretchr/testify/require"

	cfg "github.com/TencentBlueKing/bkunifylogbeat/config"
)

func TestManagerPreviewInputDoneNotifiesOnce(t *testing.T) {
	manager := &Manager{
		preview:          true,
		previewInputDone: make(chan struct{}),
		previewInputOnce: &sync.Once{},
	}

	manager.notifyPreviewInputDone()
	manager.notifyPreviewInputDone()

	select {
	case <-manager.PreviewInputDone():
	default:
		t.Fatal("preview input done channel was not closed")
	}
}

// TestAbortPreviewReleasesPublisherBeforeManagerStop reproduces the blocking
// chain from the review: a Task is stuck inside Publish, so Manager.Stop cannot
// join it until the publisher is disconnected. The stop manager seam only
// returns after stopPublish ran, so a reversed order records managerStop first
// (and then busts the stop budget) instead of passing on the elapsed bound
// alone.
func TestAbortPreviewReleasesPublisherBeforeManagerStop(t *testing.T) {
	withPreviewTimeouts(t, time.Second, 500*time.Millisecond)

	publisherReleased := make(chan struct{})
	var releaseCalls atomic.Int32
	var orderMu sync.Mutex
	var order []string
	record := func(event string) {
		orderMu.Lock()
		order = append(order, event)
		orderMu.Unlock()
	}

	withStopPublish(t, func() {
		record("stopPublish")
		releaseCalls.Add(1)
		close(publisherReleased)
	})

	// cleanupRelease unblocks a stop manager seam that is still waiting when
	// the test fails; the expected order returns as soon as publish is out.
	cleanupRelease := make(chan struct{})
	withStopManager(t, func(*Manager) error {
		record("managerStop")
		select {
		case <-publisherReleased:
			return nil
		case <-cleanupRelease:
			return nil
		}
	})
	t.Cleanup(func() { close(cleanupRelease) })

	bt := &LogBeat{done: make(chan struct{}), manager: &Manager{}}

	start := time.Now()
	err := bt.abortPreview(errors.New("console output is blocked"))
	elapsed := time.Since(start)

	require.ErrorContains(t, err, "console output is blocked")
	require.Equal(t, int32(1), releaseCalls.Load(), "abort must release the publisher exactly once")

	orderMu.Lock()
	gotOrder := append([]string(nil), order...)
	orderMu.Unlock()
	require.Equal(t, []string{"stopPublish", "managerStop"}, gotOrder,
		"abort must release the publisher before joining Manager.Stop")

	require.Less(t, elapsed, time.Second, "abort must not wait for the blocked publisher")
	require.True(t, isClosedForTest(bt.done), "abort must release the beater done channel")
}

// TestAbortPreviewReturnsWhenManagerStopStaysBlocked covers the worst case: even
// a publisher release that does not unblock the Task must not keep Run alive.
func TestAbortPreviewReturnsWhenManagerStopStaysBlocked(t *testing.T) {
	withPreviewTimeouts(t, time.Second, 50*time.Millisecond)
	withStopPublish(t, func() {})

	releaseManager := make(chan struct{})
	withStopManager(t, func(*Manager) error {
		<-releaseManager
		return nil
	})
	t.Cleanup(func() { close(releaseManager) })

	bt := &LogBeat{done: make(chan struct{}), manager: &Manager{}}

	start := time.Now()
	err := bt.abortPreview(errPreviewFinalizeTimeout)
	elapsed := time.Since(start)

	require.ErrorContains(t, err, "budget expired")
	require.GreaterOrEqual(t, elapsed, 50*time.Millisecond)
	require.Less(t, elapsed, time.Second, "abort must bound Manager.Stop")
	require.True(t, isClosedForTest(bt.done))
}

// TestFinishPreviewStopTimeoutKeepsStopBounded covers the drained-preview path:
// when Manager.Stop exceeds the preview stop budget, finishPreview must mark the
// beat aborted so a later bt.Stop from the framework signal path cannot wait for
// the same stuck Manager.Stop without a bound.
func TestFinishPreviewStopTimeoutKeepsStopBounded(t *testing.T) {
	withPreviewTimeouts(t, 200*time.Millisecond, 40*time.Millisecond)
	withWaitPublishIdle(t, func(time.Duration) error { return nil })

	releaseManager := make(chan struct{})
	withStopManager(t, func(*Manager) error {
		<-releaseManager
		return nil
	})
	t.Cleanup(func() { close(releaseManager) })

	bt := &LogBeat{done: make(chan struct{}), manager: &Manager{}}

	start := time.Now()
	err := bt.finishPreview(nil, newPreviewBudget(200*time.Millisecond))
	elapsed := time.Since(start)

	require.ErrorContains(t, err, "did not stop")
	require.True(t, bt.aborted.Load(), "a bounded stop timeout must mark the beat aborted")
	require.GreaterOrEqual(t, elapsed, 40*time.Millisecond)
	require.Less(t, elapsed, time.Second)
	require.True(t, isClosedForTest(bt.done))

	// Any later Stop must return immediately instead of joining the stuck
	// Manager.Stop a second time. Without the aborted flag this blocks until the
	// cleanup releases the seam.
	stopped := make(chan struct{})
	go func() {
		bt.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("Stop must stay bounded after the preview stop timeout")
	}
}

// TestFinishPreviewAbortsWhenAckDrainExceedsBudget verifies that the shutdown
// budget covers the ACK/console wait as well: when the drain does not finish in
// time, the forced path releases the publisher instead of calling Stop
// synchronously.
func TestFinishPreviewAbortsWhenAckDrainExceedsBudget(t *testing.T) {
	withPreviewTimeouts(t, 40*time.Millisecond, 200*time.Millisecond)

	var released atomic.Int32
	withStopPublish(t, func() { released.Add(1) })
	withWaitPublishIdle(t, func(timeout time.Duration) error {
		time.Sleep(timeout)
		return errors.New("publish drain timed out")
	})

	bt := newFinishTestLogBeat(make(chan struct{}))
	start := time.Now()
	err := bt.finishPreview(nil, newPreviewBudget(40*time.Millisecond))
	elapsed := time.Since(start)

	require.ErrorContains(t, err, "aborted")
	require.Equal(t, int32(1), released.Load())
	require.Less(t, elapsed, time.Second)
	require.True(t, isClosedForTest(bt.done))
}

func newPreviewRunConfig() cfg.Config {
	return cfg.Config{
		FileIdentifier: "inode",
		Registry: cfg.Registry{
			FlushTimeout: time.Second,
			GcFrequency:  time.Minute,
		},
		Tasks: []interface{}{
			map[string]interface{}{
				"type":   "kafka",
				"dataid": 999990001,
				"hosts":  []string{"127.0.0.1:19092"},
				"topics": []string{"preview-run"},
				"direct_read": map[string]interface{}{
					"enabled":    true,
					"max_events": 10,
					"timeout":    "1s",
				},
			},
		},
	}
}

func newPreviewRunLogBeat(t *testing.T, finalize, stop time.Duration) (*LogBeat, *Manager) {
	t.Helper()

	preparePreviewRegistrar(t)
	withPreviewTimeouts(t, finalize, stop)

	config := newPreviewRunConfig()
	manager, err := NewManager(config, make(chan struct{}))
	require.NoError(t, err)
	manager.isTestMode = func() bool { return true }
	manager.enableManagedPreview = func() error { return nil }
	manager.startTaskFunc = func(*cfg.TaskConfig, []file.State) error { return nil }

	return &LogBeat{
		done:    make(chan struct{}),
		manager: manager,
		config:  config,
	}, manager
}

// TestLogBeatRunStartsFinalizationBudgetOnInputCompletion is the regression test
// for the reviewed normal-completion hang: the finite input completes, but the
// drain marker never reaches the Task because the publisher is blocked. Run
// must abort within the fixed budget instead of waiting for PreviewDone.
func TestLogBeatRunStartsFinalizationBudgetOnInputCompletion(t *testing.T) {
	bt, manager := newPreviewRunLogBeat(t, 60*time.Millisecond, 200*time.Millisecond)

	manager.startTaskFunc = func(*cfg.TaskConfig, []file.State) error {
		// Completion fires while the manager is still starting tasks. The
		// marker is never delivered because the preview Task is blocked.
		manager.notifyPreviewInputDone()
		return nil
	}

	var released atomic.Int32
	withStopPublish(t, func() { released.Add(1) })

	start := time.Now()
	err := bt.Run()
	elapsed := time.Since(start)

	require.ErrorContains(t, err, "preview aborted")
	require.Equal(t, int32(1), released.Load(), "Run must release the publisher on abort")
	require.Less(t, elapsed, 2*time.Second)
	require.True(t, isClosedForTest(bt.done))
}

// TestLogBeatRunSignalTimeoutReturns covers the SIGTERM path from the review:
// after the shutdown signal, the budget expires without a drain marker and Run
// must still return instead of blocking in bt.Stop().
func TestLogBeatRunSignalTimeoutReturns(t *testing.T) {
	oldDone := libgsebeat.Done
	libgsebeat.Done = make(chan bool, 1)
	libgsebeat.Done <- true
	t.Cleanup(func() { libgsebeat.Done = oldDone })

	bt, _ := newPreviewRunLogBeat(t, 60*time.Millisecond, 200*time.Millisecond)

	var released atomic.Int32
	withStopPublish(t, func() { released.Add(1) })

	start := time.Now()
	err := bt.Run()
	elapsed := time.Since(start)

	require.ErrorContains(t, err, "preview aborted")
	require.Equal(t, int32(1), released.Load())
	require.Less(t, elapsed, 2*time.Second)
	require.True(t, isClosedForTest(bt.done))
}
