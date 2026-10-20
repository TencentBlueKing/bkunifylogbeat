// Tencent is pleased to support the open source community by making bkunifylogbeat 蓝鲸日志采集器 available.
//
// Copyright (C) 2021 THL A29 Limited, a Tencent company. All rights reserved.
// Licensed under the MIT License.

package input

import (
	"testing"

	"github.com/TencentBlueKing/bkmonitor-datalink/pkg/libgse/beat"
	"github.com/elastic/beats/filebeat/input/file"
	"github.com/elastic/beats/filebeat/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/TencentBlueKing/bkunifylogbeat/config"
	"github.com/TencentBlueKing/bkunifylogbeat/task/base"
	"github.com/TencentBlueKing/bkunifylogbeat/utils"
)

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
