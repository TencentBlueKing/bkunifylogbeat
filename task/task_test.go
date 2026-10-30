// Tencent is pleased to support the open source community by making bkunifylogbeat 蓝鲸日志采集器 available.
//
// Copyright (C) 2021 THL A29 Limited, a Tencent company.  All rights reserved.
//
// bkunifylogbeat 蓝鲸日志采集器 is licensed under the MIT License.
// See the LICENSE file for details.

package task

import (
	"errors"
	"testing"
	"time"

	"github.com/TencentBlueKing/bkmonitor-datalink/pkg/libgse/logp"
	libbeatlogp "github.com/elastic/beats/libbeat/logp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cfg "github.com/TencentBlueKing/bkunifylogbeat/config"
	"github.com/TencentBlueKing/bkunifylogbeat/task/base"
)

func init() {
	logp.SetLogger(libbeatlogp.L())
}

func newMarkerTask() *Task {
	return &Task{
		TaskNode: &base.TaskNode{Node: base.NewEmptyNode("task-1")},
		Config:   &cfg.TaskConfig{ID: "task-1"},
		beatDone: make(chan struct{}),
	}
}

func TestTaskDrainMarkerCompletesPreviewOnce(t *testing.T) {
	taskInst := newMarkerTask()
	done := make(chan error, 2)
	taskInst.SetPreviewCompletion(func(err error) { done <- err })

	go taskInst.Run()

	expected := errors.New("kafka read failed")
	taskInst.In <- &base.DrainMarker{TaskID: "task-1", Err: expected}

	select {
	case err := <-done:
		require.ErrorIs(t, err, expected)
	case <-time.After(time.Second):
		t.Fatal("preview completion callback was not called")
	}

	select {
	case <-taskInst.GameOver:
	case <-time.After(time.Second):
		t.Fatal("task did not stop after the drain marker")
	}

	// A second marker/callback must not close the completion twice.
	taskInst.completePreview(nil)
	select {
	case <-done:
		t.Fatal("completion callback ran twice")
	default:
	}
}

func TestTaskIgnoresMarkerForAnotherTask(t *testing.T) {
	taskInst := newMarkerTask()
	called := make(chan struct{}, 1)
	taskInst.SetPreviewCompletion(func(error) { called <- struct{}{} })

	go taskInst.Run()
	taskInst.In <- &base.DrainMarker{TaskID: "other-task"}

	select {
	case <-called:
		t.Fatal("marker for another task completed this task")
	case <-time.After(50 * time.Millisecond):
	}

	close(taskInst.beatDone)
	select {
	case <-taskInst.GameOver:
	case <-time.After(time.Second):
		t.Fatal("task did not stop after beatDone")
	}
	assert.False(t, isClosedForTest(called))
}

func TestTaskStopDoesNotCompletePreview(t *testing.T) {
	taskInst := newMarkerTask()
	called := make(chan struct{}, 1)
	taskInst.SetPreviewCompletion(func(error) { called <- struct{}{} })

	go taskInst.Run()
	close(taskInst.End)

	select {
	case <-taskInst.GameOver:
	case <-time.After(time.Second):
		t.Fatal("task did not stop after End was closed")
	}

	select {
	case <-called:
		t.Fatal("task stop completed the preview without a drain marker")
	default:
	}
}

func isClosedForTest(ch chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}
