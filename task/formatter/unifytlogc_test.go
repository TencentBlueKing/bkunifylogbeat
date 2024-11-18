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

package formatter

import (
	"testing"
	"time"

	"github.com/elastic/beats/filebeat/input/file"
	"github.com/elastic/beats/filebeat/util"
	"github.com/elastic/beats/libbeat/beat"
	"github.com/elastic/beats/libbeat/common"
	"github.com/stretchr/testify/assert"

	"github.com/TencentBlueKing/bkunifylogbeat/config"
)

func BaseFormatter(t *testing.T, taskConfig *config.TaskConfig) {

	f, err := NewUnifytlogcFormatter(taskConfig)
	if err != nil {
		panic(err)
	}

	event := &util.Data{
		Event: beat.Event{
			Timestamp: time.Now(),
			Fields: common.MapStr{
				"data": "log",
			},
		},
	}

	// 获取world_id测试用例
	testCases := map[string]int64{
		"/data/logs/test/test.log":          int64(-1),
		"/data/logs/test_111test/test.log":  int64(-1),
		"/data/logs/test_111_test/test.log": int64(-1),
		"/data/logs/test_0/test.log":        int64(0),
		"/data/logs/test_111/test.log":      int64(111),
		"/data/logs/test_888/test.log":      int64(888),
	}
	for path, worldID := range testCases {
		event.SetState(file.State{
			Source: path,
		})
		events := []*util.Data{event}
		result := f.Format(events)
		t.Log(path, worldID, result["_worldid_"])
		assert.Equal(t, worldID, result["_worldid_"])
	}
	for path, worldID := range testCases {
		event.SetState(file.State{
			Source: path,
		})
		events := []*util.Data{event}
		result := f.Format(events)
		t.Log(path, worldID, result["_worldid_"])
		assert.Equal(t, worldID, result["_worldid_"])
	}
}

func TestNewUnifytlogcFormatter(t *testing.T) {
	vars := map[string]interface{}{
		"dataid":          "999990001",
		"harvester_limit": 10,
	}
	taskConfig, err := config.CreateTaskConfig(vars)
	if err != nil {
		panic(err)
	}
	BaseFormatter(t, taskConfig)
}
