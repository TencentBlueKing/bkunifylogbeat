// Tencent is pleased to support the open source community by making bkunifylogbeat 蓝鲸日志采集器 available.
//
// Copyright (C) 2021 THL A29 Limited, a Tencent company. All rights reserved.
// Licensed under the MIT License.

package beater

import (
	"sync"
	"testing"
	"time"

	"github.com/elastic/beats/filebeat/input"
	"github.com/stretchr/testify/assert"

	cfg "github.com/TencentBlueKing/bkunifylogbeat/config"
)

func TestManagerConfigureAdaptiveScan(t *testing.T) {
	var installed input.AdaptiveScanHooks
	setterCalls := 0
	manager := &Manager{
		setAdaptiveScanHooksFunc: func(hooks input.AdaptiveScanHooks) {
			setterCalls++
			installed = hooks
		},
	}

	config := cfg.AdaptiveScanConfig{
		Enabled:          true,
		MinScanFrequency: 500 * time.Millisecond,
		ScanCPUPercent:   5,
		ControlInterval:  3 * time.Second,
	}
	err := manager.configureAdaptiveScan(config)
	assert.NoError(t, err)
	assert.NotNil(t, manager.adaptiveScan)
	assert.NotNil(t, installed.Interval)
	assert.NotNil(t, installed.Applied)
	requested := installed.Interval(1, 10*time.Second, time.Millisecond)
	assert.Equal(t, 500*time.Millisecond, requested)
	installed.Applied(1, 10*time.Second, requested, requested, time.Millisecond)

	controller := manager.adaptiveScan
	callsAfterFirstConfigure := setterCalls
	err = manager.configureAdaptiveScan(config)
	assert.NoError(t, err)
	assert.Same(t, controller, manager.adaptiveScan)
	assert.Equal(t, callsAfterFirstConfigure, setterCalls)

	err = manager.configureAdaptiveScan(cfg.AdaptiveScanConfig{Enabled: false})
	assert.NoError(t, err)
	assert.Nil(t, manager.adaptiveScan)
	assert.Nil(t, installed.Interval)
	assert.Nil(t, installed.Applied)
}

func TestManagerConfigureAdaptiveScanRejectsInvalidConfig(t *testing.T) {
	var installed input.AdaptiveScanHooks
	manager := &Manager{
		setAdaptiveScanHooksFunc: func(hooks input.AdaptiveScanHooks) {
			installed = hooks
		},
	}

	err := manager.configureAdaptiveScan(cfg.AdaptiveScanConfig{
		Enabled:          true,
		MinScanFrequency: 0,
		ScanCPUPercent:   5,
		ControlInterval:  time.Second,
	})

	assert.Error(t, err)
	assert.Nil(t, manager.adaptiveScan)
	assert.Nil(t, installed.Interval)
	assert.Nil(t, installed.Applied)
}

func TestManagerConfigureAdaptiveScanKeepsOldControllerWhenReplacementIsInvalid(t *testing.T) {
	var installed input.AdaptiveScanHooks
	manager := &Manager{
		setAdaptiveScanHooksFunc: func(hooks input.AdaptiveScanHooks) {
			installed = hooks
		},
	}
	valid := cfg.AdaptiveScanConfig{
		Enabled:          true,
		MinScanFrequency: 500 * time.Millisecond,
		ScanCPUPercent:   5,
		ControlInterval:  time.Second,
	}
	assert.NoError(t, manager.configureAdaptiveScan(valid))
	oldController := manager.adaptiveScan

	err := manager.configureAdaptiveScan(cfg.AdaptiveScanConfig{
		Enabled:          true,
		MinScanFrequency: 0,
		ScanCPUPercent:   5,
		ControlInterval:  time.Second,
	})

	assert.Error(t, err)
	assert.Same(t, oldController, manager.adaptiveScan)
	assert.NotNil(t, installed.Interval)
	assert.NotNil(t, installed.Applied)
	assert.NoError(t, manager.configureAdaptiveScan(cfg.AdaptiveScanConfig{Enabled: false}))
}

func TestManagerConfigureAdaptiveScanSerializesConcurrentReplacement(t *testing.T) {
	manager := &Manager{
		setAdaptiveScanHooksFunc: func(input.AdaptiveScanHooks) {},
	}
	configs := []cfg.AdaptiveScanConfig{
		{
			Enabled:          true,
			MinScanFrequency: 500 * time.Millisecond,
			ScanCPUPercent:   5,
			ControlInterval:  time.Second,
		},
		{
			Enabled:          true,
			MinScanFrequency: time.Second,
			ScanCPUPercent:   10,
			ControlInterval:  2 * time.Second,
		},
		{Enabled: false},
	}

	var wg sync.WaitGroup
	for index := 0; index < 30; index++ {
		wg.Add(1)
		go func(config cfg.AdaptiveScanConfig) {
			defer wg.Done()
			assert.NoError(t, manager.configureAdaptiveScan(config))
		}(configs[index%len(configs)])
	}
	wg.Wait()

	assert.NoError(t, manager.configureAdaptiveScan(cfg.AdaptiveScanConfig{Enabled: false}))
	assert.Nil(t, manager.adaptiveScan)
}
