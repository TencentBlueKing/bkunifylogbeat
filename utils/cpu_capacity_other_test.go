// Tencent is pleased to support the open source community by making bkunifylogbeat 蓝鲸日志采集器 available.
//
// Copyright (C) 2021 THL A29 Limited, a Tencent company. All rights reserved.
// Licensed under the MIT License.

//go:build !linux
// +build !linux

package utils

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdaptiveScanNonLinuxFallbackPreservesSingleCoreBudget(t *testing.T) {
	controller, err := NewAdaptiveScanController(AdaptiveScanSettings{
		MinScanFrequency: 100 * time.Millisecond,
		ScanCPUPercent:   5,
		ControlInterval:  time.Second,
	})
	require.NoError(t, err)

	assert.Equal(t, time.Second, controller.NextInterval(1, 30*time.Second, 50*time.Millisecond))
	snapshot := controller.snapshot()
	assert.InDelta(t, 0.05, snapshot.TargetDuty, 0.0001)
	assert.Zero(t, snapshot.EffectiveCores)
	assert.Equal(t, cpuCapacitySourceNonLinux, snapshot.CPUCapacitySource)
}
