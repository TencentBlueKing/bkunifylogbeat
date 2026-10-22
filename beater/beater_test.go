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
	"sync"
	"testing"

	"github.com/TencentBlueKing/bkmonitor-datalink/pkg/libgse/logp"
	bkStorage "github.com/TencentBlueKing/bkmonitor-datalink/pkg/libgse/storage"
	libbeatlogp "github.com/elastic/beats/libbeat/logp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	// Run/Stop 内部使用 logp.L，测试进程需要先初始化日志对象。
	logp.SetLogger(libbeatlogp.L())
}

// TestLogBeatStopIdempotent 验证连续调用 Stop 不 panic，且 done 只被关闭一次。
// 若第二次调用重复 close(done)，会直接触发 panic 并被 NotPanics 捕获。
func TestLogBeatStopIdempotent(t *testing.T) {
	bt := &LogBeat{done: make(chan struct{})}

	assert.NotPanics(t, func() { bt.Stop() })
	assert.NotPanics(t, func() { bt.Stop() })

	select {
	case <-bt.done:
	default:
		t.Fatal("done channel should be closed after Stop")
	}
}

// TestLogBeatStopWithNilFields 验证测试构造的 LogBeat（manager/done 为空）重复 Stop 也不 panic。
func TestLogBeatStopWithNilFields(t *testing.T) {
	bt := &LogBeat{}

	assert.NotPanics(t, func() {
		bt.Stop()
		bt.Stop()
	})
}

// TestLogBeatStopConcurrent 验证多个 goroutine 同时 Stop 时 done 仍只关闭一次。
func TestLogBeatStopConcurrent(t *testing.T) {
	bt := &LogBeat{done: make(chan struct{})}

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			bt.Stop()
		}()
	}
	wg.Wait()

	select {
	case <-bt.done:
	default:
		t.Fatal("done channel should be closed after concurrent Stop calls")
	}
}

// TestLogBeatRunReturnsStorageError 验证 Registrar.New 的存储错误会从 Run 返回：
// 内存 Storage 初始化后立即 Close，Registrar.Init 读取 localtime 时返回错误，
// 该错误不能再被 Run 吞掉，否则入口无法感知启动失败。
func TestLogBeatRunReturnsStorageError(t *testing.T) {
	oldRegistrar := Registrar
	t.Cleanup(func() {
		Registrar = oldRegistrar
		bkStorage.Close()
	})

	bkStorage.InitMemory()
	bkStorage.Close()

	bt := &LogBeat{done: make(chan struct{})}
	err := bt.Run()

	require.ErrorContains(t, err, "get localtime")
}
