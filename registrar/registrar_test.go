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
	"testing"
	"time"

	cfg "github.com/TencentBlueKing/bkunifylogbeat/config"
	"github.com/TencentBlueKing/bkunifylogbeat/tests"
	"github.com/TencentBlueKing/collector-go-sdk/v2/bkbeat/logp"
	bkStorage "github.com/TencentBlueKing/collector-go-sdk/v2/bkbeat/storage"
	"github.com/elastic/beats/filebeat/input/file"
	libbeatlogp "github.com/elastic/beats/libbeat/logp"
	"github.com/stretchr/testify/assert"
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
	})
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

func TestRegistrarIO(t *testing.T) {
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
	bkStorage.Set(timeKey, time.Now().Format(time.UnixDate), 0)

	str, _ := json.Marshal(make([]file.State, 0))
	bkStorage.Set(registrarKey, string(str), 0)
	if err != nil {
		panic(err)
	}

	registrar, err := New(cfg.Registry{
		FlushTimeout: 20 * time.Second,
		GcFrequency:  20 * time.Second,
	})
	if err != nil {
		panic(err)
	}
	err = registrar.Init()
	if err != nil {
		panic(err)
	}
	registrar.Start()
	source := "/data/logs/test.log"
	for i := 0; i < 100; i++ {
		states := make([]file.State, 0)
		for j := 0; j < 100000; j++ {
			//Step 3: 写入事件
			data := tests.MockLogEvent(source, "test")
			states = append(states, data.GetState())
		}
		registrar.Channel <- states
	}

	//Step 5: 关闭并删除文件
	registrar.Stop()
	bkStorage.Close()
	os.Remove(testRegPath)
}
