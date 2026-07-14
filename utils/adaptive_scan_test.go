// Tencent is pleased to support the open source community by making bkunifylogbeat 蓝鲸日志采集器 available.
//
// Copyright (C) 2021 THL A29 Limited, a Tencent company. All rights reserved.
// Licensed under the MIT License.

package utils

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func newTestAdaptiveScanController(t *testing.T) *AdaptiveScanController {
	t.Helper()
	controller, err := NewAdaptiveScanController(AdaptiveScanSettings{
		MinScanFrequency: 500 * time.Millisecond,
		ScanCPUPercent:   5,
		ControlInterval:  3 * time.Second,
	})
	assert.NoError(t, err)
	controller.logIntervalChange = func(AdaptiveScanIntervalLog) {}
	controller.logSnapshot = func(AdaptiveScanSnapshot) {}
	controller.logInputSnapshots = func([]AdaptiveScanIntervalLog) {}
	return controller
}

func nextAndObserve(
	controller *AdaptiveScanController,
	runnerID uint64,
	base, scanDuration time.Duration,
) time.Duration {
	requested := controller.NextInterval(runnerID, base, scanDuration)
	controller.ObserveApplied(runnerID, base, requested, requested, scanDuration)
	return requested
}

func TestAdaptiveScanControllerUsesFirstSampleImmediately(t *testing.T) {
	controller := newTestAdaptiveScanController(t)

	got := controller.NextInterval(1, 10*time.Second, time.Millisecond)

	assert.Equal(t, 500*time.Millisecond, got)
	assert.Equal(t, time.Millisecond, controller.TotalScanDuration())
}

func TestAdaptiveScanControllerKeepsIndependentEWMAByInput(t *testing.T) {
	controller := newTestAdaptiveScanController(t)

	assert.Equal(t, 2*time.Second, controller.NextInterval(1, 10*time.Second, 100*time.Millisecond))
	assert.Equal(t, 3*time.Second, controller.NextInterval(1, 10*time.Second, 200*time.Millisecond))
	assert.Equal(t, 4*time.Second, controller.NextInterval(2, 10*time.Second, 200*time.Millisecond))
}

func TestAdaptiveScanControllerClampsToConfiguredBounds(t *testing.T) {
	controller := newTestAdaptiveScanController(t)

	assert.Equal(t, 10*time.Second, controller.NextInterval(1, 10*time.Second, time.Second))
	assert.Equal(t, 200*time.Millisecond, controller.NextInterval(2, 200*time.Millisecond, time.Millisecond))
	assert.Equal(t, 10*time.Second, controller.NextInterval(3, 10*time.Second, 0))
	assert.Equal(t, 10*time.Second, controller.NextInterval(3, 10*time.Second, -time.Second))
}

func TestAdaptiveScanControllerMultiplierNeverExceedsBase(t *testing.T) {
	controller := newTestAdaptiveScanController(t)
	controller.NextInterval(1, time.Second, 40*time.Millisecond)
	controller.setMultiplier(20)

	got := controller.NextInterval(1, time.Second, 40*time.Millisecond)

	assert.Equal(t, time.Second, got)
}

func TestAdaptiveScanGovernorDeadband(t *testing.T) {
	controller := newTestAdaptiveScanController(t)
	for runnerID := uint64(1); runnerID <= 100; runnerID++ {
		nextAndObserve(controller, runnerID, 300*time.Second, 10*time.Millisecond)
	}
	controller.setMultiplier(40)

	controller.updateMultiplier()

	assert.Equal(t, float64(40), controller.Multiplier())
}

func TestAdaptiveScanGovernorConvergesWithOneHundredInputs(t *testing.T) {
	controller, err := NewAdaptiveScanController(AdaptiveScanSettings{
		MinScanFrequency: time.Second,
		ScanCPUPercent:   5,
		ControlInterval:  3 * time.Second,
	})
	assert.NoError(t, err)

	const inputs = 100
	base := 300 * time.Second
	scanDuration := 10 * time.Millisecond
	for runnerID := uint64(1); runnerID <= inputs; runnerID++ {
		nextAndObserve(controller, runnerID, base, scanDuration)
	}

	for range 2 {
		controller.updateMultiplier()
	}

	assert.InDelta(t, 20, controller.Multiplier(), 0.1)
	interval := controller.NextInterval(1, base, scanDuration)
	assert.InDelta(t, 20*time.Second, interval, float64(time.Millisecond))
	assert.InDelta(t, controller.targetDuty, float64(inputs)*float64(scanDuration)/float64(interval), 0.0001)
}

func TestAdaptiveScanGovernorConvergesAboveLegacyCap(t *testing.T) {
	controller, err := NewAdaptiveScanController(AdaptiveScanSettings{
		MinScanFrequency: time.Second,
		ScanCPUPercent:   5,
		ControlInterval:  3 * time.Second,
	})
	assert.NoError(t, err)

	const inputs = 1000
	base := 300 * time.Second
	scanDuration := 10 * time.Millisecond
	for runnerID := uint64(1); runnerID <= inputs; runnerID++ {
		nextAndObserve(controller, runnerID, base, scanDuration)
	}

	for range 3 {
		controller.updateMultiplier()
	}

	assert.InDelta(t, 200, controller.Multiplier(), 0.1)
	assert.Greater(t, controller.Multiplier(), float64(64))
	interval := controller.NextInterval(1, base, scanDuration)
	assert.InDelta(t, 200*time.Second, interval, float64(time.Millisecond))
	assert.InDelta(t, controller.targetDuty, float64(inputs)*float64(scanDuration)/float64(interval), 0.0001)
}

func TestAdaptiveScanGovernorStopsAtDynamicLimitWhenBudgetIsImpossible(t *testing.T) {
	controller, err := NewAdaptiveScanController(AdaptiveScanSettings{
		MinScanFrequency: time.Second,
		ScanCPUPercent:   5,
		ControlInterval:  3 * time.Second,
	})
	assert.NoError(t, err)

	const inputs = 1000
	base := 100 * time.Second
	scanDuration := 10 * time.Millisecond
	for runnerID := uint64(1); runnerID <= inputs; runnerID++ {
		nextAndObserve(controller, runnerID, base, scanDuration)
	}

	for range 5 {
		controller.updateMultiplier()
	}

	assert.InDelta(t, 100, controller.Multiplier(), 0.1)
	assert.Equal(t, base, controller.NextInterval(1, base, scanDuration))
	snapshot := controller.snapshot()
	assert.True(t, snapshot.BudgetSaturated)
	assert.InDelta(t, 0.10, snapshot.MinimumPossibleDuty, 0.0001)
}

func TestAdaptiveScanGovernorResetsWithoutActiveInputs(t *testing.T) {
	controller := newTestAdaptiveScanController(t)
	for runnerID := uint64(1); runnerID <= 100; runnerID++ {
		controller.NextInterval(runnerID, 10*time.Second, 10*time.Millisecond)
	}
	controller.updateMultiplier()
	assert.Greater(t, controller.Multiplier(), float64(1))

	for runnerID := uint64(1); runnerID <= 100; runnerID++ {
		controller.removeInput(runnerID)
	}
	controller.updateMultiplier()

	assert.Equal(t, float64(1), controller.Multiplier())
	assert.False(t, controller.snapshot().BudgetSaturated)
}

func TestAdaptiveScanGovernorRecoversTowardOne(t *testing.T) {
	controller, err := NewAdaptiveScanController(AdaptiveScanSettings{
		MinScanFrequency: time.Second,
		ScanCPUPercent:   5,
		ControlInterval:  3 * time.Second,
	})
	assert.NoError(t, err)
	for runnerID := uint64(1); runnerID <= 100; runnerID++ {
		nextAndObserve(controller, runnerID, 300*time.Second, 10*time.Millisecond)
	}
	controller.updateMultiplier()
	controller.updateMultiplier()
	before := controller.Multiplier()

	for runnerID := uint64(51); runnerID <= 100; runnerID++ {
		controller.removeInput(runnerID)
	}
	controller.updateMultiplier()

	assert.Less(t, controller.Multiplier(), before)
	assert.GreaterOrEqual(t, controller.Multiplier(), float64(10))
	assert.GreaterOrEqual(t, controller.Multiplier(), float64(1))
}

func TestAdaptiveScanGovernorSamplesAccumulatedDuty(t *testing.T) {
	controller := newTestAdaptiveScanController(t)
	start := time.Unix(100, 0)

	controller.observeSample(start, 0)
	controller.observeSample(start.Add(time.Second), int64(200*time.Millisecond))

	assert.InDelta(t, 0.2, math.Float64frombits(controller.lastDutyBits.Load()), 0.0001)
	assert.Equal(t, float64(1), controller.Multiplier())
}

func TestAdaptiveScanGovernorSmoothsAggregateDuty(t *testing.T) {
	controller := newTestAdaptiveScanController(t)
	start := time.Unix(100, 0)

	controller.observeSample(start, 0)
	controller.observeSample(start.Add(time.Second), int64(200*time.Millisecond))
	controller.observeSample(start.Add(2*time.Second), int64(200*time.Millisecond))

	assert.InDelta(t, 0.1, math.Float64frombits(controller.lastDutyBits.Load()), 0.0001)
	assert.Equal(t, float64(1), controller.Multiplier())
}

func TestAdaptiveScanGovernorStartStopIsIdempotent(t *testing.T) {
	controller, err := NewAdaptiveScanController(AdaptiveScanSettings{
		MinScanFrequency: 500 * time.Millisecond,
		ScanCPUPercent:   5,
		ControlInterval:  10 * time.Millisecond,
	})
	assert.NoError(t, err)

	for runnerID := uint64(1); runnerID <= 100; runnerID++ {
		controller.NextInterval(runnerID, 300*time.Second, 10*time.Millisecond)
	}
	controller.Start()
	controller.Start()

	deadline := time.Now().Add(time.Second)
	for controller.Multiplier() == 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	assert.True(t, controller.Multiplier() > 1)

	controller.Stop()
	controller.Stop()
}

func TestNewAdaptiveScanControllerRejectsInvalidSettings(t *testing.T) {
	tests := []AdaptiveScanSettings{
		{MinScanFrequency: 0, ScanCPUPercent: 5, ControlInterval: time.Second},
		{MinScanFrequency: time.Second, ScanCPUPercent: 0, ControlInterval: time.Second},
		{MinScanFrequency: time.Second, ScanCPUPercent: 101, ControlInterval: time.Second},
		{MinScanFrequency: time.Second, ScanCPUPercent: math.NaN(), ControlInterval: time.Second},
		{MinScanFrequency: time.Second, ScanCPUPercent: math.Inf(1), ControlInterval: time.Second},
		{MinScanFrequency: time.Second, ScanCPUPercent: 5, ControlInterval: 0},
	}
	for _, settings := range tests {
		_, err := NewAdaptiveScanController(settings)
		assert.Error(t, err)
	}
}

func TestAdaptiveScanInputMetadataMergesSharedInput(t *testing.T) {
	const runnerID = uint64(123)
	defer UnregisterAdaptiveScanInput(runnerID)

	RegisterAdaptiveScanInput(runnerID, AdaptiveScanInputMetadata{
		InputID: "input-a",
		TaskID:  "task-a",
		DataID:  1001,
		Paths:   []string{"/var/log/a.log"},
	})
	RegisterAdaptiveScanInput(runnerID, AdaptiveScanInputMetadata{
		InputID: "input-a",
		TaskID:  "task-b",
		DataID:  1002,
		Paths:   []string{"/var/log/a.log"},
	})

	metadata, ok := adaptiveScanInputMetadata(runnerID)
	assert.True(t, ok)
	assert.Equal(t, "input-a", metadata.InputID)
	assert.ElementsMatch(t, []string{"task-a", "task-b"}, metadata.TaskIDs)
	assert.ElementsMatch(t, []int{1001, 1002}, metadata.DataIDs)
	assert.Equal(t, []string{"/var/log/a.log"}, metadata.Paths)

	UnregisterAdaptiveScanTask(runnerID, AdaptiveScanInputMetadata{TaskID: "task-a"})
	metadata, ok = adaptiveScanInputMetadata(runnerID)
	assert.True(t, ok)
	assert.Equal(t, []string{"task-b"}, metadata.TaskIDs)
	assert.Equal(t, []int{1002}, metadata.DataIDs)

	UnregisterAdaptiveScanInput(runnerID)
	_, ok = adaptiveScanInputMetadata(runnerID)
	assert.False(t, ok)
}

func TestAdaptiveScanControllerLogsMeaningfulIntervalChanges(t *testing.T) {
	const runnerID = uint64(456)
	defer UnregisterAdaptiveScanInput(runnerID)
	RegisterAdaptiveScanInput(runnerID, AdaptiveScanInputMetadata{
		InputID: "input-a",
		TaskID:  "task-a",
		DataID:  1001,
		Paths:   []string{"/var/log/app.log"},
	})

	controller := newTestAdaptiveScanController(t)
	now := time.Unix(100, 0)
	controller.now = func() time.Time { return now }
	var records []AdaptiveScanIntervalLog
	controller.logIntervalChange = func(record AdaptiveScanIntervalLog) {
		records = append(records, record)
	}

	nextAndObserve(controller, runnerID, 10*time.Second, time.Millisecond)
	assert.Len(t, records, 1)
	assert.Equal(t, "input-a", records[0].InputID)
	assert.Equal(t, []string{"task-a"}, records[0].TaskIDs)
	assert.Equal(t, []int{1001}, records[0].DataIDs)
	assert.Equal(t, 500*time.Millisecond, records[0].RequestedInterval)
	assert.Equal(t, 500*time.Millisecond, records[0].EffectiveInterval)

	now = now.Add(10 * time.Second)
	nextAndObserve(controller, runnerID, 10*time.Second, 100*time.Millisecond)
	assert.Len(t, records, 1)

	now = now.Add(61 * time.Second)
	nextAndObserve(controller, runnerID, 10*time.Second, 100*time.Millisecond)
	assert.Len(t, records, 2)
	assert.Equal(t, 1505*time.Millisecond, records[1].EffectiveInterval)
}

func TestAdaptiveScanControllerDoesNotLogUnchangedInitialInterval(t *testing.T) {
	const runnerID = uint64(457)
	defer UnregisterAdaptiveScanInput(runnerID)
	RegisterAdaptiveScanInput(runnerID, AdaptiveScanInputMetadata{
		InputID: "input-expensive",
		TaskID:  "task-expensive",
		DataID:  1002,
	})

	controller := newTestAdaptiveScanController(t)
	var records []AdaptiveScanIntervalLog
	controller.logIntervalChange = func(record AdaptiveScanIntervalLog) {
		records = append(records, record)
	}

	assert.Equal(t, 10*time.Second, nextAndObserve(controller, runnerID, 10*time.Second, time.Second))
	assert.Empty(t, records)
}

func TestAdaptiveScanControllerRecordsFinalAppliedInterval(t *testing.T) {
	const runnerID = uint64(458)
	RegisterAdaptiveScanInput(runnerID, AdaptiveScanInputMetadata{
		InputID: "input-fallback",
		TaskID:  "task-fallback",
		DataID:  1003,
	})
	defer UnregisterAdaptiveScanInput(runnerID)
	controller := newTestAdaptiveScanController(t)
	var records []AdaptiveScanIntervalLog
	controller.logIntervalChange = func(record AdaptiveScanIntervalLog) {
		records = append(records, record)
	}
	controller.NextInterval(runnerID, 10*time.Second, time.Millisecond)

	controller.ObserveApplied(
		runnerID,
		10*time.Second,
		11*time.Second,
		10*time.Second,
		time.Millisecond,
	)

	inputs := controller.inputSnapshots()
	assert.Len(t, inputs, 1)
	assert.Equal(t, 11*time.Second, inputs[0].RequestedInterval)
	assert.Equal(t, 10*time.Second, inputs[0].EffectiveInterval)
	assert.Len(t, records, 1)
	assert.Equal(t, 11*time.Second, records[0].RequestedInterval)
	assert.Equal(t, 10*time.Second, records[0].EffectiveInterval)
}

func TestUnregisterAdaptiveScanInputRemovesControllerState(t *testing.T) {
	const runnerID = uint64(789)
	controller := newTestAdaptiveScanController(t)
	controller.NextInterval(runnerID, 10*time.Second, time.Millisecond)
	SetActiveAdaptiveScanController(controller)
	defer SetActiveAdaptiveScanController(nil)

	UnregisterAdaptiveScanInput(runnerID)

	controller.mu.Lock()
	_, ok := controller.inputs[runnerID]
	controller.mu.Unlock()
	assert.False(t, ok)
}

func TestAdaptiveScanControllerBuildsBoundedAggregateSnapshot(t *testing.T) {
	runnerIDs := []uint64{801, 802, 803}
	for index, runnerID := range runnerIDs {
		RegisterAdaptiveScanInput(runnerID, AdaptiveScanInputMetadata{
			InputID: "input-" + string(rune('a'+index)),
			TaskID:  "task-" + string(rune('a'+index)),
			DataID:  1001 + index,
			Paths:   []string{"/var/log/app.log"},
		})
		defer UnregisterAdaptiveScanInput(runnerID)
	}

	controller := newTestAdaptiveScanController(t)
	nextAndObserve(controller, runnerIDs[0], 10*time.Second, time.Millisecond)
	nextAndObserve(controller, runnerIDs[1], 10*time.Second, 100*time.Millisecond)
	nextAndObserve(controller, runnerIDs[2], 10*time.Second, time.Second)
	controller.observeSample(time.Unix(100, 0), 0)
	controller.observeSample(time.Unix(101, 0), int64(30*time.Millisecond))

	snapshot := controller.snapshot()

	assert.Equal(t, 3, snapshot.Inputs)
	assert.Equal(t, 2, snapshot.Shortened)
	assert.Equal(t, 1, snapshot.Intervals.AtMinimum)
	assert.Equal(t, 1, snapshot.Intervals.UpTo2Seconds)
	assert.Equal(t, 1, snapshot.Intervals.AtBase)
	assert.InDelta(t, 0.03, snapshot.ScanDuty, 0.0001)
	assert.Len(t, snapshot.SlowestInputs, 3)
	assert.Equal(t, runnerIDs[2], snapshot.SlowestInputs[0].RunnerID)

	inputs := controller.inputSnapshots()
	assert.Len(t, inputs, 3)
	assert.Equal(t, runnerIDs[0], inputs[0].RunnerID)
	assert.Equal(t, []string{"/var/log/app.log"}, inputs[0].Paths)
}

func TestAdaptiveScanControllerLogsPeriodicSnapshot(t *testing.T) {
	controller := newTestAdaptiveScanController(t)
	controller.snapshotInterval = 10 * time.Millisecond
	logged := make(chan AdaptiveScanSnapshot, 1)
	controller.logSnapshot = func(snapshot AdaptiveScanSnapshot) {
		logged <- snapshot
	}

	controller.Start()
	defer controller.Stop()

	select {
	case <-logged:
	case <-time.After(time.Second):
		t.Fatal("periodic adaptive scan snapshot was not logged")
	}
}
