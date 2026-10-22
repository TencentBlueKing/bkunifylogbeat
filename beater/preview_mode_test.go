// Tencent is pleased to support the open source community by making bkunifylogbeat 蓝鲸日志采集器 available.
//
// Copyright (C) 2021 THL A29 Limited, a Tencent company.  All rights reserved.
//
// bkunifylogbeat 蓝鲸日志采集器 is licensed under the MIT License.
//
// License for bkunifylogbeat 蓝鲸日志采集器:
// --------------------------------------------------------------------
// Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated
// documentation files (the "Software"), to deal in the Software without restriction, including without limitation
// the rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the Software,
// and to permit persons to whom the Software is furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all copies or substantial
// portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT
// LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN
// NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY,
// WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE
// SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.

package beater

import (
	"errors"
	"testing"
	"time"

	"github.com/elastic/beats/filebeat/input/file"
	"github.com/stretchr/testify/require"

	bkStorage "github.com/TencentBlueKing/bkmonitor-datalink/pkg/libgse/storage"

	cfg "github.com/TencentBlueKing/bkunifylogbeat/config"
	"github.com/TencentBlueKing/bkunifylogbeat/registrar"
)

func newPreviewTaskConfig() cfg.Config {
	return cfg.Config{
		Tasks: []interface{}{
			map[string]interface{}{
				"type":   "log",
				"dataid": 999990001,
				"paths":  []string{"/tmp/preview-invalid.log"},
			},
		},
	}
}

func preparePreviewRegistrar(t *testing.T) {
	t.Helper()

	oldRegistrar := Registrar
	t.Cleanup(func() {
		Registrar = oldRegistrar
		bkStorage.Close()
	})

	bkStorage.InitMemory()
	r, err := registrar.New(cfg.Registry{
		FlushTimeout: time.Second,
		GcFrequency:  time.Minute,
	}, "inode")
	require.NoError(t, err)
	Registrar = r
}

func TestManagerStartReturnsErrorInPreviewMode(t *testing.T) {
	preparePreviewRegistrar(t)

	manager, err := NewManager(newPreviewTaskConfig(), make(chan struct{}))
	require.NoError(t, err)
	require.Len(t, cfg.GetTasks(manager.config), 1)

	manager.isTestMode = func() bool { return true }
	called := 0
	manager.startTaskFunc = func(*cfg.TaskConfig, []file.State) error {
		called++
		return errors.New("invalid preview task")
	}
	t.Cleanup(func() { _ = manager.Stop() })

	err = manager.Start()
	require.ErrorContains(t, err, "invalid preview task")
	require.ErrorContains(t, err, "failed to start task")
	require.Equal(t, 1, called)
}

func TestManagerStartKeepsTolerantModeInFormalMode(t *testing.T) {
	preparePreviewRegistrar(t)

	manager, err := NewManager(newPreviewTaskConfig(), make(chan struct{}))
	require.NoError(t, err)
	manager.isTestMode = func() bool { return false }
	called := 0
	manager.startTaskFunc = func(*cfg.TaskConfig, []file.State) error {
		called++
		return errors.New("invalid formal task")
	}
	t.Cleanup(func() { _ = manager.Stop() })

	require.NoError(t, manager.Start())
	require.Equal(t, 1, called)
}

func TestLogBeatRunReturnsManagerErrorInPreviewMode(t *testing.T) {
	preparePreviewRegistrar(t)

	manager, err := NewManager(newPreviewTaskConfig(), make(chan struct{}))
	require.NoError(t, err)
	manager.isTestMode = func() bool { return true }
	manager.startTaskFunc = func(*cfg.TaskConfig, []file.State) error {
		return errors.New("invalid preview task")
	}

	bt := &LogBeat{
		done:    make(chan struct{}),
		manager: manager,
		config: cfg.Config{
			Registry: cfg.Registry{
				FlushTimeout: time.Second,
				GcFrequency:  time.Minute,
			},
		},
	}

	err = bt.Run()
	require.ErrorContains(t, err, "could not start manager")
	require.ErrorContains(t, err, "invalid preview task")
}
