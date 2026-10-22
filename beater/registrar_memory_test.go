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
	"encoding/json"
	"testing"
	"time"

	bkStorage "github.com/TencentBlueKing/bkmonitor-datalink/pkg/libgse/storage"
	"github.com/elastic/beats/filebeat/input/file"
	"github.com/stretchr/testify/require"

	cfg "github.com/TencentBlueKing/bkunifylogbeat/config"
	"github.com/TencentBlueKing/bkunifylogbeat/registrar"
)

// TestRegistrarWithMemoryStorage 验证 -T 场景下 Registrar 与内存 Storage 的组合：
// flush=1s 走 Get(localtime) 分支，flush=10s 额外走 SetFlushInterval 分支，
// Start/Stop 的定时刷新与关闭路径均落到内存实现且不 panic。
func TestRegistrarWithMemoryStorage(t *testing.T) {
	for _, flush := range []time.Duration{time.Second, 10 * time.Second} {
		t.Run(flush.String(), func(t *testing.T) {
			bkStorage.InitMemory()
			defer bkStorage.Close()

			r, err := registrar.New(cfg.Registry{
				FlushTimeout: flush,
				GcFrequency:  time.Minute,
			}, "inode")
			require.NoError(t, err)

			r.Start()
			r.Stop()

			// Stop 的 defer flushRegistry 已把 localtime 写回内存存储，
			// 关闭前应能读到合法时间值。
			str, err := bkStorage.Get("localtime")
			require.NoError(t, err)
			_, err = time.Parse(time.UnixDate, str)
			require.NoError(t, err)
		})
	}
}

// TestRegistrarMigratesV1StatesWithMemoryStorage 验证内存 Storage 下的 v1 进度迁移读取：
// 预置 localtime 与 registrar key 后，New/Init 应删除 registrar key、按 state: 前缀写入
// 新进度键，Start/Stop 全程走内存实现。
func TestRegistrarMigratesV1StatesWithMemoryStorage(t *testing.T) {
	bkStorage.InitMemory()
	defer bkStorage.Close()

	require.NoError(t, bkStorage.Set("localtime", time.Now().Format(time.UnixDate), 0))

	legacyStates := []file.State{
		{Source: "/data/logs/legacy-a.log", Offset: 10, Timestamp: time.Now(), Type: "log", Meta: map[string]string{"id": "a"}},
		{Source: "/data/logs/legacy-b.log", Offset: 20, Timestamp: time.Now(), Type: "log", Meta: map[string]string{"id": "b"}},
	}
	raw, err := json.Marshal(legacyStates)
	require.NoError(t, err)
	require.NoError(t, bkStorage.Set("registrar", string(raw), 0))

	r, err := registrar.New(cfg.Registry{
		FlushTimeout: time.Second,
		GcFrequency:  time.Minute,
	}, "inode")
	require.NoError(t, err)

	// v1 registrar key 在迁移完成后被删除。
	_, err = bkStorage.Get("registrar")
	require.ErrorIs(t, err, bkStorage.ErrNotFound)

	// 迁移后的进度以 state: 前缀写入，可读且能反序列化。
	values, err := bkStorage.List("state:")
	require.NoError(t, err)
	require.Len(t, values, len(legacyStates))
	for _, value := range values {
		var state file.State
		require.NoError(t, json.Unmarshal([]byte(value), &state))
	}

	require.Len(t, r.GetStates(), len(legacyStates))

	r.Start()
	r.Stop()
}
