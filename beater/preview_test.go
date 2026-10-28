// Tencent is pleased to support the open source community by making bkunifylogbeat 蓝鲸日志采集器 available.
//
// Copyright (C) 2021 THL A29 Limited, a Tencent company.  All rights reserved.
//
// bkunifylogbeat 蓝鲸日志采集器 is licensed under the MIT License.
// See the LICENSE file for details.

package beater

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cfg "github.com/TencentBlueKing/bkunifylogbeat/config"
)

func newTaskConfigForPreview(t *testing.T, taskType string, direct *bool) *cfg.TaskConfig {
	t.Helper()

	raw := map[string]interface{}{
		"type":   taskType,
		"dataid": 999990001,
	}
	if taskType == "kafka" {
		raw["hosts"] = []string{"127.0.0.1:19092"}
		raw["topics"] = []string{"preview-single"}
	} else {
		raw["paths"] = []string{"/tmp/preview-test.log"}
	}
	if direct != nil {
		directRaw := map[string]interface{}{"enabled": *direct}
		if *direct {
			// Disabled direct_read must not carry limits; Beats rejects that
			// combination. Only the enabled preview task gets a finite budget.
			directRaw["max_events"] = 30
			directRaw["timeout"] = "10s"
		}
		raw["direct_read"] = directRaw
	}

	task, err := cfg.CreateTaskConfig(raw)
	require.NoError(t, err)
	return task
}

func newPreviewManager(testMode bool, enableCalls *int) *Manager {
	return &Manager{
		isTestMode: func() bool { return testMode },
		enableManagedPreview: func() error {
			if enableCalls != nil {
				(*enableCalls)++
			}
			return nil
		},
	}
}

func TestPreparePreviewValidatesDirectRead(t *testing.T) {
	enabled := true

	t.Run("valid kafka preview", func(t *testing.T) {
		enableCalls := 0
		manager := newPreviewManager(true, &enableCalls)
		task := newTaskConfigForPreview(t, "kafka", &enabled)
		tasks := map[string]*cfg.TaskConfig{task.ID: task}

		require.NoError(t, manager.preparePreview(tasks))
		assert.True(t, manager.preview)
		assert.Equal(t, task.ID, manager.previewTaskID)
		assert.NotNil(t, manager.PreviewDone())
		assert.Equal(t, 1, enableCalls)
		assert.NoError(t, manager.PreviewErr())
	})

	t.Run("normal mode rejects direct_read", func(t *testing.T) {
		enableCalls := 0
		manager := newPreviewManager(false, &enableCalls)
		task := newTaskConfigForPreview(t, "kafka", &enabled)
		tasks := map[string]*cfg.TaskConfig{task.ID: task}

		err := manager.preparePreview(tasks)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "only allowed with -T")
		assert.False(t, manager.preview)
		assert.Equal(t, 0, enableCalls)
	})

	t.Run("direct_read requires kafka", func(t *testing.T) {
		manager := newPreviewManager(true, nil)
		task := newTaskConfigForPreview(t, "log", &enabled)
		tasks := map[string]*cfg.TaskConfig{task.ID: task}

		err := manager.preparePreview(tasks)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "requires a kafka task")
	})

	t.Run("preview requires exactly one task", func(t *testing.T) {
		manager := newPreviewManager(true, nil)
		directTask := newTaskConfigForPreview(t, "kafka", &enabled)
		otherTask := newTaskConfigForPreview(t, "log", nil)
		tasks := map[string]*cfg.TaskConfig{
			directTask.ID: directTask,
			otherTask.ID:  otherTask,
		}

		err := manager.preparePreview(tasks)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "exactly one task")
	})

	t.Run("disabled direct_read does not enable preview", func(t *testing.T) {
		disabled := false
		enableCalls := 0
		manager := newPreviewManager(true, &enableCalls)
		task := newTaskConfigForPreview(t, "kafka", &disabled)
		tasks := map[string]*cfg.TaskConfig{task.ID: task}

		require.NoError(t, manager.preparePreview(tasks))
		assert.False(t, manager.preview)
		assert.Nil(t, manager.PreviewDone())
		assert.Equal(t, 0, enableCalls)
	})

	t.Run("no direct_read at all", func(t *testing.T) {
		manager := newPreviewManager(true, nil)
		task := newTaskConfigForPreview(t, "log", nil)
		tasks := map[string]*cfg.TaskConfig{task.ID: task}

		require.NoError(t, manager.preparePreview(tasks))
		assert.False(t, manager.preview)
		assert.Nil(t, manager.PreviewDone())
	})
}

func TestValidateDirectTaskOnReloadPath(t *testing.T) {
	enabled := true
	task := newTaskConfigForPreview(t, "kafka", &enabled)

	normal := newPreviewManager(false, nil)
	err := normal.validateDirectTask(task)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "only allowed with -T")

	preview := newPreviewManager(true, nil)
	preview.preview = true
	preview.previewTaskID = task.ID
	require.NoError(t, preview.validateDirectTask(task))

	preview.previewTaskID = "another-task"
	err = preview.validateDirectTask(task)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not match")
}

func TestPreviewDoneNotifiesOnce(t *testing.T) {
	manager := &Manager{
		preview:     true,
		previewDone: make(chan struct{}),
	}

	first := errors.New("first")
	manager.notifyPreviewDone(first)
	manager.notifyPreviewDone(errors.New("second"))

	select {
	case <-manager.PreviewDone():
	default:
		t.Fatal("preview done channel was not closed")
	}
	assert.Equal(t, first, manager.PreviewErr())
}

func withWaitPublishIdle(t *testing.T, fn func(time.Duration) error) {
	t.Helper()
	old := waitPublishIdle
	waitPublishIdle = fn
	t.Cleanup(func() { waitPublishIdle = old })
}

func withStopPublish(t *testing.T, fn func()) {
	t.Helper()
	old := stopPublish
	stopPublish = fn
	t.Cleanup(func() { stopPublish = old })
}

func withPreviewTimeouts(t *testing.T, finalize, stop time.Duration) {
	t.Helper()
	oldFinalize, oldStop := previewFinalizeTimeout, previewStopTimeout
	previewFinalizeTimeout, previewStopTimeout = finalize, stop
	t.Cleanup(func() {
		previewFinalizeTimeout, previewStopTimeout = oldFinalize, oldStop
	})
}

func newPreviewBudget(timeout time.Duration) *previewFinalizeBudget {
	return newPreviewFinalizeBudget(timeout)
}

func isClosedForTest(ch chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

func newFinishTestLogBeat(previewDone chan struct{}) *LogBeat {
	manager := &Manager{preview: true, previewDone: previewDone, previewTaskID: "task-1"}
	return &LogBeat{
		done:    make(chan struct{}),
		manager: manager,
	}
}

func TestFinishPreviewWaitsForPublishedEvents(t *testing.T) {
	withPreviewTimeouts(t, 200*time.Millisecond, time.Second)

	t.Run("success", func(t *testing.T) {
		withWaitPublishIdle(t, func(time.Duration) error { return nil })
		bt := newFinishTestLogBeat(make(chan struct{}))
		require.NoError(t, bt.finishPreview(nil, newPreviewBudget(200*time.Millisecond)))
	})

	t.Run("wait error becomes terminal error", func(t *testing.T) {
		expected := errors.New("console flush failed")
		withWaitPublishIdle(t, func(time.Duration) error { return expected })
		bt := newFinishTestLogBeat(make(chan struct{}))
		require.ErrorIs(t, bt.finishPreview(nil, newPreviewBudget(200*time.Millisecond)), expected)
	})

	t.Run("kafka error is preserved over wait result", func(t *testing.T) {
		expected := errors.New("kafka read failed")
		withWaitPublishIdle(t, func(time.Duration) error { return errors.New("drain failed") })
		bt := newFinishTestLogBeat(make(chan struct{}))
		require.ErrorIs(t, bt.finishPreview(expected, newPreviewBudget(200*time.Millisecond)), expected)
	})
}

func TestFinishPreviewAfterSignal(t *testing.T) {
	withPreviewTimeouts(t, time.Second, 200*time.Millisecond)

	t.Run("signal is a failure even after a clean drain", func(t *testing.T) {
		withWaitPublishIdle(t, func(time.Duration) error { return nil })
		done := make(chan struct{})
		close(done)
		bt := newFinishTestLogBeat(done)
		err := bt.finishPreviewAfterSignal(newPreviewBudget(time.Second))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "canceled by shutdown signal")
	})

	t.Run("signal wait timeout aborts instead of hanging", func(t *testing.T) {
		publishReleased := false
		withStopPublish(t, func() { publishReleased = true })
		withWaitPublishIdle(t, func(time.Duration) error { return nil })
		bt := newFinishTestLogBeat(make(chan struct{}))

		start := time.Now()
		err := bt.finishPreviewAfterSignal(newPreviewBudget(50 * time.Millisecond))
		elapsed := time.Since(start)

		require.ErrorContains(t, err, "did not drain after shutdown signal")
		require.True(t, publishReleased, "abort must release the publisher")
		require.Less(t, elapsed, time.Second, "signal timeout must not block Run")
		require.True(t, isClosedForTest(bt.done), "done must be released on abort")
	})
}
