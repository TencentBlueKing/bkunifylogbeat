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
	"time"

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

func TestFieldExtractionAndDeduplicationConfig(t *testing.T) {
	withoutDeduplication, err := CreateTaskConfig(map[string]interface{}{
		"dataid": 999990100,
		"field_extraction": map[string]interface{}{
			"pattern": `(?P<traceID>\d+)`,
		},
	})
	assert.NoError(t, err)
	assert.Nil(t, withoutDeduplication.EnabledDeduplication())

	vars := map[string]interface{}{
		"dataid": "999990101",
		"field_extraction": map[string]interface{}{
			"pattern":       `trace=(?P<traceID>\d+);proc=(?P<proc>[A-Z]+)`,
			"deduplication": map[string]interface{}{"enabled": true},
		},
	}

	taskConfig, err := CreateTaskConfig(vars)
	assert.NoError(t, err)
	assert.Equal(t, []string{"traceID", "proc"}, taskConfig.FieldExtractionCaptureNames())
	deduplication := taskConfig.EnabledDeduplication()
	assert.NotNil(t, deduplication)
	assert.Equal(t, DefaultDeduplicationWindow, deduplication.Window)
	assert.Equal(t, DefaultDeduplicationMaxKeys, deduplication.MaxKeys)
	assert.Equal(t, DefaultDeduplicationMaxTotalKeys, deduplication.MaxTotalKeys)

	custom, err := CreateTaskConfig(map[string]interface{}{
		"dataid": "999990102",
		"field_extraction": map[string]interface{}{
			"pattern": `(?P<traceID>\d+)`,
			"deduplication": map[string]interface{}{
				"enabled":        true,
				"window":         "2s",
				"max_keys":       16,
				"max_total_keys": 8,
			},
		},
	})
	assert.NoError(t, err)
	deduplication = custom.EnabledDeduplication()
	assert.Equal(t, 2*time.Second, deduplication.Window)
	assert.Equal(t, 16, deduplication.MaxKeys)
	assert.Equal(t, 8, deduplication.MaxTotalKeys)
}

func TestFieldExtractionParticipatesInFilterAndProcessorIdentity(t *testing.T) {
	first, err := CreateTaskConfig(map[string]interface{}{
		"dataid": "999990111",
		"field_extraction": map[string]interface{}{
			"pattern":       `trace=(?P<traceID>\d+)`,
			"deduplication": map[string]interface{}{"enabled": false},
		},
	})
	assert.NoError(t, err)

	second, err := CreateTaskConfig(map[string]interface{}{
		"dataid": "999990111",
		"field_extraction": map[string]interface{}{
			"pattern":       `request=(?P<traceID>\d+)`,
			"deduplication": map[string]interface{}{"enabled": false},
		},
	})
	assert.NoError(t, err)

	assert.NotEqual(t, first.ID, second.ID)
	assert.NotEqual(t, first.FilterID, second.FilterID)
	assert.NotEqual(t, first.ProcessorID, second.ProcessorID)
	assert.Equal(t, first.InputID, second.InputID)
}

func TestEnabledDeduplicationUsesTaskScopedFilterAndProcessorIdentity(t *testing.T) {
	legacyFirst, err := CreateTaskConfig(map[string]interface{}{"dataid": 999990119})
	assert.NoError(t, err)
	legacySecond, err := CreateTaskConfig(map[string]interface{}{"dataid": 999990120})
	assert.NoError(t, err)
	assert.Equal(t, legacyFirst.SenderID, legacySecond.SenderID)
	assert.Equal(t, legacyFirst.ProcessorID, legacySecond.ProcessorID)

	first, err := CreateTaskConfig(map[string]interface{}{
		"dataid":        999990121,
		"package_count": 10,
		"field_extraction": map[string]interface{}{
			"pattern":       `trace=(?P<traceID>\d+)`,
			"deduplication": map[string]interface{}{"enabled": true},
		},
	})
	assert.NoError(t, err)
	second, err := CreateTaskConfig(map[string]interface{}{
		"dataid":        999990121,
		"package_count": 20,
		"field_extraction": map[string]interface{}{
			"pattern":       `trace=(?P<traceID>\d+)`,
			"deduplication": map[string]interface{}{"enabled": true},
		},
	})
	assert.NoError(t, err)

	assert.NotEqual(t, first.SenderID, second.SenderID)
	assert.NotEqual(t, first.ProcessorID, second.ProcessorID)
	assert.NotEqual(t, first.FilterID, second.FilterID)
	assert.Equal(t, first.InputID, second.InputID)
}

func TestDisabledDeduplicationKeepsExistingProcessorSharing(t *testing.T) {
	first, err := CreateTaskConfig(map[string]interface{}{
		"dataid":        999990123,
		"package_count": 10,
		"field_extraction": map[string]interface{}{
			"pattern":       `trace=(?P<traceID>\d+)`,
			"deduplication": map[string]interface{}{"enabled": false},
		},
	})
	assert.NoError(t, err)
	second, err := CreateTaskConfig(map[string]interface{}{
		"dataid":        999990123,
		"package_count": 20,
		"field_extraction": map[string]interface{}{
			"pattern":       `trace=(?P<traceID>\d+)`,
			"deduplication": map[string]interface{}{"enabled": false},
		},
	})
	assert.NoError(t, err)
	assert.NotEqual(t, first.SenderID, second.SenderID)
	assert.Equal(t, first.ProcessorID, second.ProcessorID)
	assert.Equal(t, first.FilterID, second.FilterID)
}

func TestFieldExtractionConfigValidation(t *testing.T) {
	testCases := []struct {
		name    string
		config  map[string]interface{}
		message string
	}{
		{
			name: "empty pattern",
			config: map[string]interface{}{
				"field_extraction": map[string]interface{}{"pattern": ""},
			},
			message: "field_extraction.pattern cannot be empty",
		},
		{
			name: "unsupported output format",
			config: map[string]interface{}{
				"output_format": "v1",
				"field_extraction": map[string]interface{}{
					"pattern": `(?P<traceID>\d+)`,
				},
			},
			message: "field_extraction requires output_format v2",
		},
		{
			name: "invalid pattern",
			config: map[string]interface{}{
				"field_extraction": map[string]interface{}{"pattern": `(?P<traceID>`},
			},
			message: "compile field_extraction.pattern",
		},
		{
			name: "anonymous capture",
			config: map[string]interface{}{
				"field_extraction": map[string]interface{}{"pattern": `(\d+)`},
			},
			message: "requires every capture group to be named",
		},
		{
			name: "mixed named and anonymous captures",
			config: map[string]interface{}{
				"field_extraction": map[string]interface{}{"pattern": `(?P<id>\d+)-(\d+)`},
			},
			message: "requires every capture group to be named",
		},
		{
			name: "duplicate capture name",
			config: map[string]interface{}{
				"field_extraction": map[string]interface{}{"pattern": `(?P<id>\d+)-(?P<id>\d+)`},
			},
			message: "duplicate capture name",
		},
		{
			name: "dedup enabled is required",
			config: map[string]interface{}{
				"field_extraction": map[string]interface{}{
					"pattern":       `(?P<id>\d+)`,
					"deduplication": map[string]interface{}{},
				},
			},
			message: "field_extraction.deduplication.enabled is required",
		},
		{
			name: "invalid total limit",
			config: map[string]interface{}{
				"field_extraction": map[string]interface{}{
					"pattern": `(?P<id>\d+)`,
					"deduplication": map[string]interface{}{
						"enabled":        true,
						"max_total_keys": -1,
					},
				},
			},
			message: "max_total_keys must be greater than zero",
		},
	}

	for index, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			testCase.config["dataid"] = 999990200 + index
			_, err := CreateTaskConfig(testCase.config)
			assert.ErrorContains(t, err, testCase.message)
		})
	}
}
