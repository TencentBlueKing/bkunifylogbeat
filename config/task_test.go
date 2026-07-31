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

package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestTaskConfig_Same 测试任务解析
func TestTaskConfig_Same(t *testing.T) {
	vars := map[string]interface{}{
		"dataid":          "999990001",
		"harvester_limit": 10,
	}

	taskConfig1, _ := CreateTaskConfig(vars)
	taskConfig2, _ := CreateTaskConfig(vars)
	assert.True(t, taskConfig1.Same(taskConfig2))

	vars2 := map[string]interface{}{
		"dataid":          "999990002",
		"harvester_limit": 10,
	}
	taskConfig3, _ := CreateTaskConfig(vars2)
	assert.False(t, taskConfig1.Same(taskConfig3))
}

// TestKafkaTaskConfig 测试kafka任务配置透传
func TestKafkaTaskConfig(t *testing.T) {
	vars := map[string]interface{}{
		"dataid":         "999990001",
		"type":           "kafka",
		"hosts":          []string{"kafka-1:9093"},
		"topics":         []string{"logs"},
		"group_id":       "bkunifylogbeat_999990001",
		"username":       "collector",
		"password":       "secret",
		"sasl_mechanism": "SCRAM-SHA-512",
		"initial_offset": "newest",
	}

	taskConfig, err := CreateTaskConfig(vars)
	assert.NoError(t, err)
	assert.Equal(t, "kafka", taskConfig.Type)

	// kafka 专属字段不参与 TaskConfig 结构，需保留在 RawConfig 中透传给 kafka input
	mechanism, err := taskConfig.RawConfig.String("sasl_mechanism", -1)
	assert.NoError(t, err)
	assert.Equal(t, "SCRAM-SHA-512", mechanism)

	var kafkaCfg struct {
		Hosts []string `config:"hosts"`
	}
	assert.NoError(t, taskConfig.RawConfig.Unpack(&kafkaCfg))
	assert.Equal(t, []string{"kafka-1:9093"}, kafkaCfg.Hosts)
}

func TestLoadMetaFile(t *testing.T) {
	f, err := os.CreateTemp("", "meta.file")
	assert.NoError(t, err)

	content := []byte(`
k1=v1
k2="v2"
k3 = "v3"
k4= "v4"
k5= "v5=foo"
k5.test/gt-hh= "test"
#
foobar
`)

	f.Write(content)
	defer os.Remove(f.Name())

	meta := loadMetaFile(f.Name())
	excepted := map[string]string{
		"k1":            "v1",
		"k2":            "v2",
		"k3":            "v3",
		"k4":            "v4",
		"k5":            "v5=foo",
		"k5_test_gt_hh": "test",
	}

	assert.Equal(t, excepted, meta)
}
