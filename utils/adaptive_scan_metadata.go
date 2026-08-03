// Tencent is pleased to support the open source community by making bkunifylogbeat 蓝鲸日志采集器 available.
//
// Copyright (C) 2021 THL A29 Limited, a Tencent company. All rights reserved.
// Licensed under the MIT License.

package utils

import (
	"sort"
	"sync"
	"sync/atomic"
)

// AdaptiveScanInputMetadata 描述与 Filebeat Runner 关联的任务配置。
// 多个任务可能共享同一个 input Runner。
type AdaptiveScanInputMetadata struct {
	InputID string
	TaskID  string
	DataID  int
	Paths   []string
}

type adaptiveScanInputMetadataSnapshot struct {
	InputID string
	TaskIDs []string
	DataIDs []int
	Paths   []string
}

type adaptiveScanInputMetadataState struct {
	inputID string
	tasks   map[string]AdaptiveScanInputMetadata
}

// metadata registry 只服务诊断日志，按 Runner 聚合同一 input 下的多个任务；
// active controller 则用于在 Runner 销毁时同步清理控制器中的局部计算状态。
var adaptiveScanMetadataRegistry = struct {
	sync.RWMutex
	inputs map[uint64]*adaptiveScanInputMetadataState
}{
	inputs: make(map[uint64]*adaptiveScanInputMetadataState),
}

var activeAdaptiveScanController atomic.Pointer[AdaptiveScanController]

// SetActiveAdaptiveScanController 设置接收 Runner 清理通知的当前控制器。
func SetActiveAdaptiveScanController(controller *AdaptiveScanController) {
	activeAdaptiveScanController.Store(controller)
}

// RegisterAdaptiveScanInput 将任务元数据关联到 Runner。
func RegisterAdaptiveScanInput(runnerID uint64, metadata AdaptiveScanInputMetadata) {
	adaptiveScanMetadataRegistry.Lock()
	defer adaptiveScanMetadataRegistry.Unlock()

	state, ok := adaptiveScanMetadataRegistry.inputs[runnerID]
	if !ok {
		state = &adaptiveScanInputMetadataState{
			tasks: make(map[string]AdaptiveScanInputMetadata),
		}
		adaptiveScanMetadataRegistry.inputs[runnerID] = state
	}
	if metadata.InputID != "" {
		state.inputID = metadata.InputID
	}
	metadata.Paths = append([]string(nil), metadata.Paths...)
	state.tasks[metadata.TaskID] = metadata
}

// UnregisterAdaptiveScanTask 从共享 Runner 的元数据中移除一个任务。
func UnregisterAdaptiveScanTask(runnerID uint64, metadata AdaptiveScanInputMetadata) {
	adaptiveScanMetadataRegistry.Lock()
	defer adaptiveScanMetadataRegistry.Unlock()

	// Task 停止不等于 Runner 停止：共享 input 仍可能继续服务其他任务，
	// 因此这里只移除当前 Task，最后一个 Task 离开时才删除 Runner 元数据。
	state, ok := adaptiveScanMetadataRegistry.inputs[runnerID]
	if !ok {
		return
	}
	delete(state.tasks, metadata.TaskID)
	if len(state.tasks) == 0 {
		delete(adaptiveScanMetadataRegistry.inputs, runnerID)
	}
}

// UnregisterAdaptiveScanInput 移除与 Runner 关联的全部元数据。
func UnregisterAdaptiveScanInput(runnerID uint64) {
	adaptiveScanMetadataRegistry.Lock()
	delete(adaptiveScanMetadataRegistry.inputs, runnerID)
	adaptiveScanMetadataRegistry.Unlock()

	// 不在 registry 锁内调用 controller，避免把两套状态的锁顺序耦合在一起。
	if controller := activeAdaptiveScanController.Load(); controller != nil {
		controller.removeInput(runnerID)
	}
}

func adaptiveScanInputMetadata(runnerID uint64) (adaptiveScanInputMetadataSnapshot, bool) {
	adaptiveScanMetadataRegistry.RLock()
	state, ok := adaptiveScanMetadataRegistry.inputs[runnerID]
	if !ok {
		adaptiveScanMetadataRegistry.RUnlock()
		return adaptiveScanInputMetadataSnapshot{}, false
	}

	snapshot := adaptiveScanInputMetadataSnapshot{
		InputID: state.inputID,
		TaskIDs: make([]string, 0, len(state.tasks)),
	}
	dataIDs := make(map[int]struct{})
	paths := make(map[string]struct{})
	for _, metadata := range state.tasks {
		if metadata.TaskID != "" {
			snapshot.TaskIDs = append(snapshot.TaskIDs, metadata.TaskID)
		}
		if metadata.DataID != 0 {
			dataIDs[metadata.DataID] = struct{}{}
		}
		for _, path := range metadata.Paths {
			if path != "" {
				paths[path] = struct{}{}
			}
		}
	}
	for dataID := range dataIDs {
		snapshot.DataIDs = append(snapshot.DataIDs, dataID)
	}
	for path := range paths {
		snapshot.Paths = append(snapshot.Paths, path)
	}
	adaptiveScanMetadataRegistry.RUnlock()

	sort.Strings(snapshot.TaskIDs)
	sort.Ints(snapshot.DataIDs)
	sort.Strings(snapshot.Paths)
	return snapshot, true
}
