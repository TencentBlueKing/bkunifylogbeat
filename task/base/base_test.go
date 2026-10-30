// Tencent is pleased to support the open source community by making bkunifylogbeat 蓝鲸日志采集器 available.
//
// Copyright (C) 2021 THL A29 Limited, a Tencent company.  All rights reserved.
//
// bkunifylogbeat 蓝鲸日志采集器 is licensed under the MIT License.
// See the LICENSE file for details.

package base

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestForwardDrainRoutesOnlyToOwningTask(t *testing.T) {
	root := NewEmptyNode("input")
	root.Outs["filter-1"] = make(chan interface{}, 1)
	root.Outs["filter-2"] = make(chan interface{}, 1)

	filter1 := NewEmptyNode("filter-1")
	filter2 := NewEmptyNode("filter-2")
	root.AddTaskNode(filter1, &TaskNode{Node: NewEmptyNode("task-1")})
	root.AddTaskNode(filter2, &TaskNode{Node: NewEmptyNode("task-2")})

	assert.ElementsMatch(t, []string{"filter-1"}, root.NextNodeIDsForTask("task-1"))

	root.ForwardDrain(&DrainMarker{TaskID: "task-1"})
	select {
	case marker := <-root.Outs["filter-1"]:
		require.IsType(t, &DrainMarker{}, marker)
		assert.Equal(t, "task-1", marker.(*DrainMarker).TaskID)
	default:
		t.Fatal("drain marker was not forwarded to the owning task")
	}

	select {
	case <-root.Outs["filter-2"]:
		t.Fatal("drain marker leaked to another task")
	case <-time.After(20 * time.Millisecond):
	}
}
