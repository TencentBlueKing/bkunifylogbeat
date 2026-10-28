// Tencent is pleased to support the open source community by making bkunifylogbeat 蓝鲸日志采集器 available.
//
// Copyright (C) 2021 THL A29 Limited, a Tencent company. All rights reserved.
// Licensed under the MIT License.

package input

import (
	"testing"
	"time"

	"github.com/TencentBlueKing/bkmonitor-datalink/pkg/libgse/beat"
	"github.com/TencentBlueKing/bkmonitor-datalink/pkg/libgse/logp"
	"github.com/elastic/beats/filebeat/input/file"
	"github.com/elastic/beats/filebeat/util"
	libbeatlogp "github.com/elastic/beats/libbeat/logp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/TencentBlueKing/bkunifylogbeat/config"
	"github.com/TencentBlueKing/bkunifylogbeat/task/base"
	"github.com/TencentBlueKing/bkunifylogbeat/utils"
)

func init() {
	logp.SetLogger(libbeatlogp.L())
}

func TestAdaptiveScanMetadataFromTask(t *testing.T) {
	taskConfig := &config.TaskConfig{
		ID:      "task-a",
		InputID: "input-a",
		DataID:  1001,
		Paths:   []string{"/var/log/app.log"},
	}

	assert.Equal(t, utils.AdaptiveScanInputMetadata{
		InputID: "input-a",
		TaskID:  "task-a",
		DataID:  1001,
		Paths:   []string{"/var/log/app.log"},
	}, adaptiveScanMetadataFromTask(taskConfig))
}

func TestOnEventForwardsFinalStateAfterInputClosed(t *testing.T) {
	node := base.NewEmptyNode("input-a")
	close(node.End)

	var sent beat.Event
	in := &Input{
		Node: node,
		stateSender: func(event beat.Event) {
			sent = event
		},
	}
	state := file.State{Source: "/logs/app.log", Offset: 42}
	data := &util.Data{Event: beat.Event{Fields: nil}}
	data.SetState(state)

	require.True(t, in.OnEvent(data))
	assert.Nil(t, sent.Fields)
	assert.Equal(t, state, sent.Private)
}

func TestOnEventRejectsLogAfterInputClosed(t *testing.T) {
	node := base.NewEmptyNode("input-a")
	close(node.End)
	in := &Input{Node: node}
	data := &util.Data{Event: beat.Event{Fields: beat.MapStr{"data": "late log"}}}

	assert.False(t, in.OnEvent(data))
}

type fakeCompletionSource struct {
	completion chan struct{}
	err        error
}

func (f *fakeCompletionSource) Completion() (<-chan struct{}, bool) {
	return f.completion, true
}

func (f *fakeCompletionSource) CompletionErr() error {
	return f.err
}

type unsupportedCompletionSource struct{}

func (*unsupportedCompletionSource) Completion() (<-chan struct{}, bool) {
	return nil, false
}

func (*unsupportedCompletionSource) CompletionErr() error {
	return nil
}

func TestWatchCompletionInjectsDrainMarker(t *testing.T) {
	node := base.NewEmptyNode("input-a")
	source := &fakeCompletionSource{completion: make(chan struct{})}
	in := &Input{Node: node, completionSource: source}

	filter := base.NewEmptyNode("filter-a")
	in.AddOutput(filter)
	in.AddTaskNode(filter, &base.TaskNode{Node: base.NewEmptyNode("task-a")})

	go in.Run()
	in.WatchCompletion("task-a", nil)
	close(source.completion)

	select {
	case raw := <-filter.In:
		marker, ok := raw.(*base.DrainMarker)
		require.True(t, ok, "completion must inject a DrainMarker")
		assert.Equal(t, "task-a", marker.TaskID)
	case <-time.After(time.Second):
		t.Fatal("drain marker was not injected after completion")
	}

	close(node.End)
	select {
	case <-node.GameOver:
	case <-time.After(time.Second):
		t.Fatal("input did not stop")
	}
}

// TestWatchCompletionNotifiesBeforeDrainDelivery covers the blocking chain:
// the coordinator must learn that the finite input completed even when the
// drain marker cannot be delivered because the downstream pipeline is blocked.
// Run is intentionally not started, so enqueueDrain cannot complete.
func TestWatchCompletionNotifiesBeforeDrainDelivery(t *testing.T) {
	node := base.NewEmptyNode("input-a")
	source := &fakeCompletionSource{completion: make(chan struct{})}
	in := &Input{Node: node, completionSource: source}

	notified := make(chan struct{})
	in.WatchCompletion("task-a", func() { close(notified) })
	close(source.completion)

	select {
	case <-notified:
	case <-time.After(time.Second):
		t.Fatal("completion notification waited for the blocked drain marker")
	}

	// Release the goroutine blocked in enqueueDrain.
	close(node.End)
}

// TestWatchCompletionReportsUnsupportedSource verifies the fallback: an input
// without a completion signal must not leave the preview waiting forever.
func TestWatchCompletionReportsUnsupportedSource(t *testing.T) {
	for name, source := range map[string]completionSource{
		"nil source":         nil,
		"unsupported source": &unsupportedCompletionSource{},
	} {
		t.Run(name, func(t *testing.T) {
			node := base.NewEmptyNode("input-a")
			in := &Input{Node: node, completionSource: source}

			filter := base.NewEmptyNode("filter-a")
			in.AddOutput(filter)
			in.AddTaskNode(filter, &base.TaskNode{Node: base.NewEmptyNode("task-a")})
			go in.Run()

			notified := make(chan struct{})
			in.WatchCompletion("task-a", func() { close(notified) })

			select {
			case <-notified:
			case <-time.After(time.Second):
				t.Fatal("input completion notification was not fired for an unsupported source")
			}

			select {
			case raw := <-filter.In:
				marker, ok := raw.(*base.DrainMarker)
				require.True(t, ok, "fallback must inject a DrainMarker")
				assert.Equal(t, "task-a", marker.TaskID)
				require.Error(t, marker.Err)
			case <-time.After(time.Second):
				t.Fatal("fallback drain marker was not injected")
			}

			close(node.End)
			select {
			case <-node.GameOver:
			case <-time.After(time.Second):
				t.Fatal("input did not stop")
			}
		})
	}
}
