// Tencent is pleased to support the open source community by making bkunifylogbeat 蓝鲸日志采集器 available.
//
// Copyright (C) 2021 THL A29 Limited, a Tencent company. All rights reserved.
// Licensed under the MIT License.

//go:build !windows
// +build !windows

package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdaptiveScanCgroupBudgetEndToEnd(t *testing.T) {
	tests := []struct {
		version      int
		quotaCores   float64
		wantTarget   float64
		wantInterval time.Duration
		wantSource   string
	}{
		{version: 1, quotaCores: 0.1, wantTarget: 0.005, wantInterval: 10 * time.Second, wantSource: cpuCapacitySourceCgroupV1Quota},
		{version: 1, quotaCores: 0.5, wantTarget: 0.025, wantInterval: 2 * time.Second, wantSource: cpuCapacitySourceCgroupV1Quota},
		{version: 1, quotaCores: 1, wantTarget: 0.05, wantInterval: time.Second, wantSource: cpuCapacitySourceCgroupV1Quota},
		{version: 1, quotaCores: 2, wantTarget: 0.05, wantInterval: time.Second, wantSource: cpuCapacitySourceCgroupV1Quota},
		{version: 2, quotaCores: 0.1, wantTarget: 0.005, wantInterval: 10 * time.Second, wantSource: cpuCapacitySourceCgroupV2Quota},
		{version: 2, quotaCores: 0.5, wantTarget: 0.025, wantInterval: 2 * time.Second, wantSource: cpuCapacitySourceCgroupV2Quota},
		{version: 2, quotaCores: 1, wantTarget: 0.05, wantInterval: time.Second, wantSource: cpuCapacitySourceCgroupV2Quota},
		{version: 2, quotaCores: 2, wantTarget: 0.05, wantInterval: time.Second, wantSource: cpuCapacitySourceCgroupV2Quota},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("v%d_%.1f_cores", tt.version, tt.quotaCores), func(t *testing.T) {
			reader, _ := newTestCgroupCapacityReader(t, tt.version, tt.quotaCores)
			controller, err := newAdaptiveScanController(AdaptiveScanSettings{
				MinScanFrequency: 100 * time.Millisecond,
				ScanCPUPercent:   5,
				ControlInterval:  time.Second,
			}, reader)
			require.NoError(t, err)

			interval := controller.NextInterval(1, 30*time.Second, 50*time.Millisecond)
			snapshot := controller.snapshot()

			assert.InDelta(t, tt.wantInterval, interval, float64(time.Nanosecond))
			assert.InDelta(t, tt.wantTarget, snapshot.TargetDuty, 0.00001)
			assert.InDelta(t, tt.quotaCores, snapshot.EffectiveCores, 0.00001)
			assert.Equal(t, tt.wantSource, snapshot.CPUCapacitySource)
		})
	}
}

func TestCgroupCPUCapacityUsesTightestV2HierarchyLimit(t *testing.T) {
	cgroupRoot, procRoot := newTestCgroupRoots(t)
	leaf := filepath.Join(cgroupRoot, "kubepods", "pod1", "container1")
	pod := filepath.Dir(leaf)
	parent := filepath.Dir(pod)

	writeCgroupTestFile(t, filepath.Join(procRoot, "self", "cgroup"), "0::/kubepods/pod1/container1\n")
	writeCgroupTestFile(t, filepath.Join(procRoot, "self", "mountinfo"),
		"36 25 0:32 / /sys/fs/cgroup rw - cgroup2 cgroup rw\n")
	writeCgroupTestFile(t, filepath.Join(leaf, "cpu.max"), "max 100000\n")
	writeCgroupTestFile(t, filepath.Join(pod, "cpu.max"), "200000 100000\n")
	writeCgroupTestFile(t, filepath.Join(parent, "cpu.max"), "50000 100000\n")
	writeCgroupTestFile(t, filepath.Join(cgroupRoot, "cpu.max"), "100000 100000\n")
	writeCgroupTestFile(t, filepath.Join(leaf, "cpuset.cpus.effective"), "0-3\n")

	capacity, err := newCgroupCPUCapacityReaderWithRoots(cgroupRoot, procRoot).Capacity()

	require.NoError(t, err)
	assert.True(t, capacity.Limited)
	assert.InDelta(t, 0.5, capacity.EffectiveCores, 0.0001)
	assert.Equal(t, cpuCapacitySourceCgroupV2Quota, capacity.Source)
}

func TestCgroupCPUCapacityUsesTightestV1HierarchyLimit(t *testing.T) {
	cgroupRoot, procRoot := newTestCgroupRoots(t)
	cpuMount := filepath.Join(cgroupRoot, "cpu")
	cpusetMount := filepath.Join(cgroupRoot, "cpuset")
	leaf := filepath.Join(cpuMount, "workloads", "collector")
	parent := filepath.Dir(leaf)

	writeCgroupTestFile(t, filepath.Join(procRoot, "self", "cgroup"),
		"2:cpu,cpuacct:/workloads/collector\n3:cpuset:/workloads/collector\n")
	writeCgroupTestFile(t, filepath.Join(procRoot, "self", "mountinfo"),
		"36 25 0:32 / /sys/fs/cgroup/cpu rw - cgroup cgroup rw,cpu,cpuacct\n"+
			"37 25 0:33 / /sys/fs/cgroup/cpuset rw - cgroup cgroup rw,cpuset\n")
	writeCgroupTestFile(t, filepath.Join(leaf, "cpu.cfs_quota_us"), "-1\n")
	writeCgroupTestFile(t, filepath.Join(leaf, "cpu.cfs_period_us"), "100000\n")
	writeCgroupTestFile(t, filepath.Join(parent, "cpu.cfs_quota_us"), "50000\n")
	writeCgroupTestFile(t, filepath.Join(parent, "cpu.cfs_period_us"), "100000\n")
	writeCgroupTestFile(t, filepath.Join(cpuMount, "cpu.cfs_quota_us"), "100000\n")
	writeCgroupTestFile(t, filepath.Join(cpuMount, "cpu.cfs_period_us"), "100000\n")
	writeCgroupTestFile(t, filepath.Join(cpusetMount, "workloads", "collector", "cpuset.cpus"), "0-3\n")

	capacity, err := newCgroupCPUCapacityReaderWithRoots(cgroupRoot, procRoot).Capacity()

	require.NoError(t, err)
	assert.InDelta(t, 0.5, capacity.EffectiveCores, 0.0001)
	assert.Equal(t, cpuCapacitySourceCgroupV1Quota, capacity.Source)
}

func TestCgroupCPUCapacitySupportsNamespacedMountRoot(t *testing.T) {
	cgroupRoot, procRoot := newTestCgroupRoots(t)
	writeCgroupTestFile(t, filepath.Join(procRoot, "self", "cgroup"), "0::/\n")
	writeCgroupTestFile(t, filepath.Join(procRoot, "self", "mountinfo"),
		"36 25 0:32 /kubepods/pod1/container1 /sys/fs/cgroup rw - cgroup2 cgroup rw\n")
	writeCgroupTestFile(t, filepath.Join(cgroupRoot, "cpu.max"), "10000 100000\n")

	capacity, err := newCgroupCPUCapacityReaderWithRoots(cgroupRoot, procRoot).Capacity()

	require.NoError(t, err)
	assert.InDelta(t, 0.1, capacity.EffectiveCores, 0.0001)
	assert.Equal(t, cpuCapacitySourceCgroupV2Quota, capacity.Source)
}

func TestCgroupCPUCapacitySelectsMatchingV2Mount(t *testing.T) {
	for _, unrelatedFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("unrelated_first_%t", unrelatedFirst), func(t *testing.T) {
			cgroupRoot, procRoot := newTestCgroupRoots(t)
			leaf := filepath.Join(cgroupRoot, "kubepods", "pod1", "container1")
			writeCgroupTestFile(t, filepath.Join(procRoot, "self", "cgroup"),
				"0::/kubepods/pod1/container1\n")

			validMount := "36 25 0:32 / /sys/fs/cgroup rw - cgroup2 cgroup rw\n"
			unrelatedMount := "37 25 0:32 /kubepods/pod2 /sys/fs/cgroup/other rw - cgroup2 cgroup rw\n"
			mountInfo := validMount + unrelatedMount
			if unrelatedFirst {
				mountInfo = unrelatedMount + validMount
			}
			writeCgroupTestFile(t, filepath.Join(procRoot, "self", "mountinfo"), mountInfo)
			writeCgroupTestFile(t, filepath.Join(leaf, "cpu.max"), "10000 100000\n")

			capacity, err := newCgroupCPUCapacityReaderWithRoots(cgroupRoot, procRoot).Capacity()

			require.NoError(t, err)
			assert.InDelta(t, 0.1, capacity.EffectiveCores, 0.00001)
			assert.True(t, capacity.Limited)
			assert.Equal(t, cpuCapacitySourceCgroupV2Quota, capacity.Source)
		})
	}
}

func TestCgroupCPUCapacitySelectsMatchingV1Mount(t *testing.T) {
	for _, unrelatedFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("unrelated_first_%t", unrelatedFirst), func(t *testing.T) {
			cgroupRoot, procRoot := newTestCgroupRoots(t)
			leaf := filepath.Join(cgroupRoot, "cpu", "workloads", "collector")
			writeCgroupTestFile(t, filepath.Join(procRoot, "self", "cgroup"),
				"2:cpu,cpuacct:/workloads/collector\n")

			validMount := "36 25 0:32 / /sys/fs/cgroup/cpu rw - cgroup cgroup rw,cpu,cpuacct\n"
			unrelatedMount := "37 25 0:32 /other /sys/fs/cgroup/other rw - cgroup cgroup rw,cpu,cpuacct\n"
			mountInfo := validMount + unrelatedMount
			if unrelatedFirst {
				mountInfo = unrelatedMount + validMount
			}
			writeCgroupTestFile(t, filepath.Join(procRoot, "self", "mountinfo"), mountInfo)
			writeCgroupTestFile(t, filepath.Join(leaf, "cpu.cfs_quota_us"), "10000\n")
			writeCgroupTestFile(t, filepath.Join(leaf, "cpu.cfs_period_us"), "100000\n")

			capacity, err := newCgroupCPUCapacityReaderWithRoots(cgroupRoot, procRoot).Capacity()

			require.NoError(t, err)
			assert.InDelta(t, 0.1, capacity.EffectiveCores, 0.00001)
			assert.True(t, capacity.Limited)
			assert.Equal(t, cpuCapacitySourceCgroupV1Quota, capacity.Source)
		})
	}
}

func TestCgroupCPUCapacityUsesCPUSetWhenTighter(t *testing.T) {
	reader, cgroupLeaf := newTestCgroupCapacityReader(t, 2, 4)
	writeCgroupTestFile(t, filepath.Join(cgroupLeaf, "cpuset.cpus.effective"), "2\n")

	capacity, err := reader.Capacity()

	require.NoError(t, err)
	assert.InDelta(t, 1, capacity.EffectiveCores, 0.0001)
	assert.Equal(t, cpuCapacitySourceCgroupV2Set, capacity.Source)
}

func TestCgroupCPUCapacityDoesNotHideQuotaReadError(t *testing.T) {
	cgroupRoot, procRoot := newTestCgroupRoots(t)
	leaf := filepath.Join(cgroupRoot, "service")
	writeCgroupTestFile(t, filepath.Join(procRoot, "self", "cgroup"), "0::/service\n")
	writeCgroupTestFile(t, filepath.Join(procRoot, "self", "mountinfo"),
		"36 25 0:32 / /sys/fs/cgroup rw - cgroup2 cgroup rw\n")
	require.NoError(t, os.MkdirAll(filepath.Join(leaf, "cpu.max"), 0o755))
	writeCgroupTestFile(t, filepath.Join(leaf, "cpuset.cpus.effective"), "0-3\n")

	_, err := newCgroupCPUCapacityReaderWithRoots(cgroupRoot, procRoot).Capacity()

	assert.Error(t, err)
}

func TestCgroupCPUCapacityUnlimitedPreservesConfiguredBudget(t *testing.T) {
	cgroupRoot, procRoot := newTestCgroupRoots(t)
	leaf := filepath.Join(cgroupRoot, "service")
	writeCgroupTestFile(t, filepath.Join(procRoot, "self", "cgroup"), "0::/service\n")
	writeCgroupTestFile(t, filepath.Join(procRoot, "self", "mountinfo"),
		"36 25 0:32 / /sys/fs/cgroup rw - cgroup2 cgroup rw\n")
	writeCgroupTestFile(t, filepath.Join(leaf, "cpu.max"), "max 100000\n")

	reader := newCgroupCPUCapacityReaderWithRoots(cgroupRoot, procRoot)
	controller, err := newAdaptiveScanController(AdaptiveScanSettings{
		MinScanFrequency: 100 * time.Millisecond,
		ScanCPUPercent:   5,
		ControlInterval:  time.Second,
	}, reader)
	require.NoError(t, err)

	snapshot := controller.snapshot()
	assert.InDelta(t, 0.05, snapshot.TargetDuty, 0.0001)
	assert.Zero(t, snapshot.EffectiveCores)
	assert.Equal(t, cpuCapacitySourceUnlimited, snapshot.CPUCapacitySource)
}

func TestCgroupV1CPUCapacityUnlimitedPreservesConfiguredBudget(t *testing.T) {
	cgroupRoot, procRoot := newTestCgroupRoots(t)
	leaf := filepath.Join(cgroupRoot, "cpu", "service")
	writeCgroupTestFile(t, filepath.Join(procRoot, "self", "cgroup"), "2:cpu,cpuacct:/service\n")
	writeCgroupTestFile(t, filepath.Join(procRoot, "self", "mountinfo"),
		"36 25 0:32 / /sys/fs/cgroup/cpu rw - cgroup cgroup rw,cpu,cpuacct\n")
	writeCgroupTestFile(t, filepath.Join(leaf, "cpu.cfs_quota_us"), "-1\n")
	writeCgroupTestFile(t, filepath.Join(leaf, "cpu.cfs_period_us"), "100000\n")

	reader := newCgroupCPUCapacityReaderWithRoots(cgroupRoot, procRoot)
	controller, err := newAdaptiveScanController(AdaptiveScanSettings{
		MinScanFrequency: 100 * time.Millisecond,
		ScanCPUPercent:   5,
		ControlInterval:  time.Second,
	}, reader)
	require.NoError(t, err)

	snapshot := controller.snapshot()
	assert.InDelta(t, 0.05, snapshot.TargetDuty, 0.0001)
	assert.Zero(t, snapshot.EffectiveCores)
	assert.Equal(t, cpuCapacitySourceUnlimited, snapshot.CPUCapacitySource)
}

func TestAdaptiveScanRefreshesRuntimeCPUQuotaAndKeepsLastGoodValue(t *testing.T) {
	reader, cgroupLeaf := newTestCgroupCapacityReader(t, 2, 0.5)
	controller, err := newAdaptiveScanController(AdaptiveScanSettings{
		MinScanFrequency: 100 * time.Millisecond,
		ScanCPUPercent:   5,
		ControlInterval:  time.Second,
	}, reader)
	require.NoError(t, err)
	assert.Equal(t, 2*time.Second, controller.NextInterval(1, 30*time.Second, 50*time.Millisecond))

	writeCgroupTestFile(t, filepath.Join(cgroupLeaf, "cpu.max"), "10000 100000\n")
	controller.updateMultiplier()
	controller.removeInput(1)
	controller.updateMultiplier()
	assert.InDelta(t, 10*time.Second, controller.NextInterval(2, 30*time.Second, 50*time.Millisecond), float64(time.Nanosecond))
	assert.InDelta(t, 0.005, controller.snapshot().TargetDuty, 0.0001)

	writeCgroupTestFile(t, filepath.Join(cgroupLeaf, "cpu.max"), "invalid\n")
	assert.Error(t, controller.refreshCPUCapacity())
	assert.InDelta(t, 0.005, controller.snapshot().TargetDuty, 0.0001)
}

func TestParseCPUMax(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  float64
		ok    bool
	}{
		{name: "default period", value: "200000", want: 2, ok: true},
		{name: "explicit period", value: "50000 100000", want: 0.5, ok: true},
		{name: "unlimited", value: "max 100000"},
		{name: "zero period", value: "100000 0"},
		{name: "invalid quota", value: "invalid 100000"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseCPUMax(tt.value)
			assert.Equal(t, tt.ok, ok)
			assert.InDelta(t, tt.want, got, 0.0001)
		})
	}
}

func TestCountCPUSet(t *testing.T) {
	count, err := parseCPUSet("0-2,4,6")
	require.NoError(t, err)
	assert.Equal(t, 5, count)

	count, err = parseCPUSet("")
	require.NoError(t, err)
	assert.Zero(t, count)

	_, err = parseCPUSet("invalid")
	assert.Error(t, err)
}

func newTestCgroupCapacityReader(
	t *testing.T,
	version int,
	quotaCores float64,
) (*cgroupCPUCapacityReader, string) {
	t.Helper()
	cgroupRoot, procRoot := newTestCgroupRoots(t)
	relative := filepath.Join("workloads", "collector")

	switch version {
	case 1:
		cpuMount := filepath.Join(cgroupRoot, "cpu")
		cpusetMount := filepath.Join(cgroupRoot, "cpuset")
		writeCgroupTestFile(t, filepath.Join(procRoot, "self", "cgroup"),
			"2:cpu,cpuacct:/workloads/collector\n3:cpuset:/workloads/collector\n")
		writeCgroupTestFile(t, filepath.Join(procRoot, "self", "mountinfo"),
			"36 25 0:32 / /sys/fs/cgroup/cpu rw - cgroup cgroup rw,cpu,cpuacct\n"+
				"37 25 0:33 / /sys/fs/cgroup/cpuset rw - cgroup cgroup rw,cpuset\n")
		leaf := filepath.Join(cpuMount, relative)
		writeCgroupTestFile(t, filepath.Join(leaf, "cpu.cfs_quota_us"),
			fmt.Sprintf("%.0f\n", quotaCores*100000))
		writeCgroupTestFile(t, filepath.Join(leaf, "cpu.cfs_period_us"), "100000\n")
		writeCgroupTestFile(t, filepath.Join(cpusetMount, relative, "cpuset.cpus"), "0-7\n")
		return newCgroupCPUCapacityReaderWithRoots(cgroupRoot, procRoot), leaf
	case 2:
		writeCgroupTestFile(t, filepath.Join(procRoot, "self", "cgroup"), "0::/workloads/collector\n")
		writeCgroupTestFile(t, filepath.Join(procRoot, "self", "mountinfo"),
			"36 25 0:32 / /sys/fs/cgroup rw - cgroup2 cgroup rw\n")
		leaf := filepath.Join(cgroupRoot, relative)
		writeCgroupTestFile(t, filepath.Join(leaf, "cpu.max"),
			fmt.Sprintf("%.0f 100000\n", quotaCores*100000))
		writeCgroupTestFile(t, filepath.Join(leaf, "cpuset.cpus.effective"), "0-7\n")
		return newCgroupCPUCapacityReaderWithRoots(cgroupRoot, procRoot), leaf
	default:
		t.Fatalf("unsupported cgroup version %d", version)
		return nil, ""
	}
}

func newTestCgroupRoots(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	return filepath.Join(root, "sys", "fs", "cgroup"), filepath.Join(root, "proc")
}

func writeCgroupTestFile(t *testing.T, filename, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(filename), 0o755))
	require.NoError(t, os.WriteFile(filename, []byte(content), 0o644))
}
