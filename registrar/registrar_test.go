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
