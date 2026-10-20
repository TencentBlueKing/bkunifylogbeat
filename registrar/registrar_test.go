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

package registrar

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/TencentBlueKing/bkmonitor-datalink/pkg/libgse/logp"
	bkStorage "github.com/TencentBlueKing/bkmonitor-datalink/pkg/libgse/storage"
	"github.com/elastic/beats/filebeat/input/file"
	beatfile "github.com/elastic/beats/libbeat/common/file"
	libbeatlogp "github.com/elastic/beats/libbeat/logp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cfg "github.com/TencentBlueKing/bkunifylogbeat/config"
	"github.com/TencentBlueKing/bkunifylogbeat/tests"
)

func init() {
	logp.SetLogger(libbeatlogp.L())
}

func TestRegistrar(t *testing.T) {
	testRegPath, err := filepath.Abs("../tests/registrar.bkpipe.db")
	if err != nil {
		panic(err)
	}
	// Step 1: 如果文件存在则直接删除
	_, err = os.Stat(testRegPath)
	if err != nil {
		if os.IsExist(err) {
			err = os.Remove(testRegPath)
			if err != nil {
				panic(err)
			}
		}
	}

	//Step 2: 初始化registrar
	err = bkStorage.Init(testRegPath, nil)
	if err != nil {
		panic(err)
	}

	registrar, err := New(cfg.Registry{
		FlushTimeout: 1 * time.Second,
		GcFrequency:  1 * time.Second,
	}, "inode")
	if err != nil {
		panic(err)
	}
	err = registrar.Init()
	if err != nil {
		panic(err)
	}
	registrar.Start()

	//Step 3: 写入事件
	source := "/data/logs/test.log"
	data := tests.MockLogEvent(source, "test")

	//Step 4：查看事件是否正常
	states := make([]file.State, 0)
	states = append(states, data.GetState())
	registrar.Channel <- states

	time.Sleep(2 * time.Second)

	regStates := registrar.GetStates()
	assert.Equal(t, len(regStates), 1)
	assert.Equal(t, regStates[0].Source, source)

	//Step 5: 关闭并删除文件
	registrar.Stop()
	bkStorage.Close()
	os.Remove(testRegPath)
}

func TestStateFileIdentifierInode(t *testing.T) {
	testRegPath, err := filepath.Abs("../tests/registrar.bkpipe.db")
	if err != nil {
		panic(err)
	}
	// Step 1: 如果文件存在则直接删除
	_, err = os.Stat(testRegPath)
	if err != nil {
		if os.IsExist(err) {
			err = os.Remove(testRegPath)
			if err != nil {
				panic(err)
			}
		}
	}

	//Step 2: 初始化registrar
	err = bkStorage.Init(testRegPath, nil)
	if err != nil {
		panic(err)
	}

	registrar, err := New(cfg.Registry{
		FlushTimeout: 1 * time.Second,
		GcFrequency:  1 * time.Second,
	}, "inode")
	if err != nil {
		panic(err)
	}

	//Step 3.1: 测试唯一性校验 inode
	now := time.Now()
	tenMinuteAgo := now.Add(-10 * time.Minute)

	states := []file.State{
		{Source: "/data/logs/old.log", Offset: 10, Timestamp: tenMinuteAgo, FileStateOS: beatfile.StateOS{Inode: 100, Device: 900}},
		{Source: "/data/logs/new.log", Offset: 20, Timestamp: now, FileStateOS: beatfile.StateOS{Inode: 101, Device: 900}},
		{Source: "/data/logs/new.log", Offset: 30, Timestamp: now, FileStateOS: beatfile.StateOS{Inode: 100, Device: 900}},
	}
	newStates := registrar.deduplicateStates(states)
	// sort newStates  by Offset
	sort.Slice(newStates, func(i, j int) bool {
		return newStates[i].Offset < newStates[j].Offset
	})
	assert.Equal(t, len(newStates), 2)

	assert.Equal(t, newStates[0].Source, "/data/logs/new.log")
	assert.Equal(t, newStates[0].Offset, int64(20))
	assert.Equal(t, newStates[0].ID(), "101-900")

	assert.Equal(t, newStates[1].Source, "/data/logs/new.log")
	assert.Equal(t, newStates[1].Offset, int64(30))
	assert.Equal(t, newStates[1].ID(), "100-900")

	// step 3.2: 测试唯一性校验 inode_path
	registrar.fileIdentifier = "inode_path"

	states = []file.State{
		{Source: "/data/logs/old.log", Offset: 10, Timestamp: tenMinuteAgo, FileStateOS: beatfile.StateOS{Inode: 100, Device: 900}},
		{Source: "/data/logs/new.log", Offset: 20, Timestamp: now, FileStateOS: beatfile.StateOS{Inode: 101, Device: 900}},
		{Source: "/data/logs/new.log", Offset: 30, Timestamp: now, FileStateOS: beatfile.StateOS{Inode: 100, Device: 900}},
	}
	newStates = registrar.deduplicateStates(states)
	// sort newStates  by Offset
	sort.Slice(newStates, func(i, j int) bool {
		return newStates[i].Offset < newStates[j].Offset
	})

	assert.Equal(t, len(newStates), 3)

	assert.Equal(t, newStates[0].Source, "/data/logs/old.log")
	assert.Equal(t, newStates[0].Offset, int64(10))
	assert.Equal(t, newStates[0].ID(), "100-900:/data/logs/old.log")

	assert.Equal(t, newStates[1].Source, "/data/logs/new.log")
	assert.Equal(t, newStates[1].Offset, int64(20))
	assert.Equal(t, newStates[1].ID(), "101-900:/data/logs/new.log")

	assert.Equal(t, newStates[2].Source, "/data/logs/new.log")
	assert.Equal(t, newStates[2].Offset, int64(30))
	assert.Equal(t, newStates[2].ID(), "100-900:/data/logs/new.log")

	// step 3.3: 测试唯一性校验 path
	registrar.fileIdentifier = "path"

	states = []file.State{
		{Source: "/data/logs/old.log", Offset: 10, Timestamp: tenMinuteAgo, FileStateOS: beatfile.StateOS{Inode: 100, Device: 900}},
		{Source: "/data/logs/new.log", Offset: 20, Timestamp: tenMinuteAgo, FileStateOS: beatfile.StateOS{Inode: 101, Device: 900}},
		{Source: "/data/logs/new.log", Offset: 30, Timestamp: now, FileStateOS: beatfile.StateOS{Inode: 100, Device: 900}},
	}
	newStates = registrar.deduplicateStates(states)
	sort.Slice(newStates, func(i, j int) bool {
		return newStates[i].Offset < newStates[j].Offset
	})
	assert.Equal(t, len(newStates), 2)

	assert.Equal(t, newStates[0].Source, "/data/logs/old.log")
	assert.Equal(t, newStates[0].Offset, int64(10))
	assert.Equal(t, newStates[0].ID(), "/data/logs/old.log")

	assert.Equal(t, newStates[1].Source, "/data/logs/new.log")
	assert.Equal(t, newStates[1].Offset, int64(30))
	assert.Equal(t, newStates[1].ID(), "/data/logs/new.log")

	//Step 4: 关闭并删除文件
	registrar.Stop()
	bkStorage.Close()
	os.Remove(testRegPath)
}

func TestGCStatesRespectsGraceUntil(t *testing.T) {
	tests := []struct {
		name       string
		graceUntil *time.Time
		wantCount  int
	}{
		{
			name:       "future grace retains unmanaged state",
			graceUntil: timePtr(time.Now().Add(time.Hour)),
			wantCount:  1,
		},
		{
			name:       "expired grace removes unmanaged state",
			graceUntil: timePtr(time.Now().Add(-time.Hour)),
			wantCount:  0,
		},
		{
			name:      "no grace keeps legacy immediate cleanup",
			wantCount: 0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			initTestStorage(t)
			state := file.State{
				Source:     "/data/logs/grace.log",
				Finished:   true,
				TTL:        stateNotManage,
				GraceUntil: test.graceUntil,
				FileStateOS: beatfile.StateOS{
					Inode:  100,
					Device: 900,
				},
			}
			r := newGCRegistrar(state)

			r.gcStates()

			assert.Equal(t, test.wantCount, r.states.Count())
			if test.wantCount == 1 {
				retained := r.states.GetStates()
				require.Len(t, retained, 1)
				assert.Equal(t, time.Duration(stateNotManage), retained[0].TTL)
				assert.Equal(t, test.graceUntil, retained[0].GraceUntil)
			}
		})
	}
}

func TestGCStatesRespectsPersistedGraceAfterReset(t *testing.T) {
	initTestStorage(t)
	graceUntil := time.Now().Add(time.Hour).UTC().Truncate(time.Nanosecond)
	original := file.State{
		Source:     "/data/logs/persisted.log",
		Finished:   true,
		TTL:        -1,
		GraceUntil: &graceUntil,
		FileStateOS: beatfile.StateOS{
			Inode:  101,
			Device: 900,
		},
	}
	encoded, err := json.Marshal(original)
	require.NoError(t, err)

	var restored file.State
	require.NoError(t, json.Unmarshal(encoded, &restored))
	reset := ResetStates([]file.State{restored})
	require.Len(t, reset, 1)
	assert.Equal(t, time.Duration(stateNotManage), reset[0].TTL)
	require.NotNil(t, reset[0].GraceUntil)
	assert.Equal(t, graceUntil, *reset[0].GraceUntil)

	r := newGCRegistrar(reset[0])
	r.gcStates()
	assert.Equal(t, 1, r.states.Count(), "future persisted grace must survive collector restart GC")
}

func TestLegacyStateGraceMigrationIsScopedPersistedAndNotExtended(t *testing.T) {
	initTestStorage(t)

	eligible := legacyState(
		"/var/host/data/bcs/service/kubelet/pods/old/volume-subpaths/app-log/service/4/service.log",
		100, -1,
	)
	explicitCleanup := legacyState(
		"/var/host/data/bcs/service/kubelet/pods/deleted/volume-subpaths/app-log/service/4/deleted.log",
		101, 0,
	)
	outOfScope := legacyState("/var/host/var/log/other.log", 102, -1)
	persistLegacyStates(t, eligible, explicitCleanup, outOfScope)

	config := cfg.Registry{
		FlushTimeout:            time.Second,
		GcFrequency:             time.Minute,
		MigrationGrace:          time.Hour,
		MigrationSourcePatterns: []string{`/volume-subpaths/app-log/`},
	}
	first, err := New(config, file.IdentifierInode)
	require.NoError(t, err)

	states := statesBySource(first.GetStates())
	require.NotNil(t, states[eligible.Source].GraceUntil, "eligible old state must receive migration grace")
	assert.Nil(t, states[explicitCleanup.Source].GraceUntil, "explicit cleanup state must not be revived")
	assert.Nil(t, states[outOfScope.Source].GraceUntil, "out-of-scope state must keep legacy behaviour")
	assert.Equal(t, int64(24), states[eligible.Source].Offset)

	firstDeadline := *states[eligible.Source].GraceUntil
	migrated := states[eligible.Source]
	storedValue, err := bkStorage.Get(stateKeyPrefix + migrated.ID())
	require.NoError(t, err)
	var stored file.State
	require.NoError(t, json.Unmarshal([]byte(storedValue), &stored))
	require.NotNil(t, stored.GraceUntil, "migration must persist before the first GC")
	assert.Equal(t, firstDeadline, *stored.GraceUntil)

	second, err := New(config, file.IdentifierInode)
	require.NoError(t, err)
	restarted := statesBySource(second.GetStates())[eligible.Source]
	require.NotNil(t, restarted.GraceUntil)
	assert.Equal(t, firstDeadline, *restarted.GraceUntil, "collector restart must not extend migration grace")

	scanned := legacyState("/new-pod-path/app/log/service.log", 100, -1)
	previous := second.states.FindPrevious(scanned)
	require.False(t, previous.IsEmpty(), "same inode must still find the migrated legacy state")
	assert.Equal(t, int64(24), previous.Offset, "migration must preserve the old offset")
}

func TestLegacyStateGraceMigrationExpiresAndCleansUp(t *testing.T) {
	initTestStorage(t)

	state := legacyState(
		"/var/host/data/bcs/service/kubelet/pods/old/volume-subpaths/app-log/service/4/service.log",
		200, -1,
	)
	persistLegacyStates(t, state)

	r, err := New(cfg.Registry{
		FlushTimeout:            time.Second,
		GcFrequency:             time.Minute,
		MigrationGrace:          time.Nanosecond,
		MigrationSourcePatterns: []string{`/volume-subpaths/app-log/`},
	}, file.IdentifierInode)
	require.NoError(t, err)
	require.NotNil(t, r.GetStates()[0].GraceUntil)

	r.gcRequired = true
	r.gcStates()
	assert.Zero(t, r.states.Count(), "expired migration grace must fall back to normal GC")
}

func legacyState(source string, inode uint64, ttl time.Duration) file.State {
	return file.State{
		Source:    source,
		Offset:    24,
		Timestamp: time.Now(),
		TTL:       ttl,
		Type:      "log",
		FileStateOS: beatfile.StateOS{
			Inode:  inode,
			Device: 900,
		},
		FileIdentifier: file.IdentifierInode,
	}
}

func persistLegacyStates(t *testing.T, states ...file.State) {
	t.Helper()
	require.NoError(t, bkStorage.Set(timeKey, time.Now().Format(time.UnixDate), 0))
	for _, state := range states {
		encoded, err := json.Marshal(state)
		require.NoError(t, err)
		require.NoError(t, bkStorage.Set(stateKeyPrefix+state.ID(), string(encoded), 0))
	}
}

func statesBySource(states []file.State) map[string]file.State {
	result := make(map[string]file.State, len(states))
	for _, state := range states {
		result[state.Source] = state
	}
	return result
}

func newGCRegistrar(state file.State) *Registrar {
	states := file.NewStates()
	states.SetStates([]file.State{state})
	return &Registrar{
		states:       states,
		gcRequired:   true,
		stateIDCache: make(map[string]struct{}),
	}
}

func initTestStorage(t *testing.T) {
	t.Helper()
	require.NoError(t, bkStorage.Init(filepath.Join(t.TempDir(), "registrar.bkpipe.db"), nil))
	t.Cleanup(bkStorage.Close)
}

func timePtr(value time.Time) *time.Time {
	return &value
}
