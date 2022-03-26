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

package filter

import (
	"testing"

	cfg "github.com/TencentBlueKing/bkunifylogbeat/config"
	"github.com/TencentBlueKing/bkunifylogbeat/tests"
	"github.com/stretchr/testify/assert"
)

//TestFilter: 测试原过滤器兼容
func TestFilter(t *testing.T) {
	vars := map[string]interface{}{
		"dataid":    "999990001",
		"delimiter": "|",
		"filters": []cfg.FilterConfig{
			cfg.FilterConfig{
				Conditions: []cfg.ConditionConfig{
					cfg.ConditionConfig{
						Index: -1,
						Key:   "test",
						Op:    "=",
					},
				},
			},
		},
	}
	config, err := cfg.CreateTaskConfig(vars)
	if err != nil {
		panic(err)
	}
	filter := NewFilters(config)

	//case 1: 必须包含test才会上报
	data := tests.MockLogEvent("/test.log", "test")
	event := filter.Run(&data.Event)
	assert.NotNil(t, event)

	//case 2: 没有包含test所以会直接过滤
	data = tests.MockLogEvent("/test.log", "data")
	event = filter.Run(&data.Event)
	assert.Nil(t, event)

	//case 3: 组合条件上报
	vars["filters"] = []cfg.FilterConfig{
		cfg.FilterConfig{
			Conditions: []cfg.ConditionConfig{
				cfg.ConditionConfig{
					Index: 1,
					Key:   "debug",
					Op:    "!=",
				},
				cfg.ConditionConfig{
					Index: 2,
					Key:   "test",
					Op:    "=",
				},
			},
		},
	}
	config, err = cfg.CreateTaskConfig(vars)
	if err != nil {
		panic(err)
	}
	filter = NewFilters(config)

	//case 3: 只上报非debug并且包含test的事件
	data = tests.MockLogEvent("/test.log", "info|test")
	event = filter.Run(&data.Event)
	assert.NotNil(t, event)

	data = tests.MockLogEvent("/test.log", "debug|test")
	event = filter.Run(&data.Event)
	assert.Nil(t, event)

	data = tests.MockLogEvent("/test.log", "info|data")
	event = filter.Run(&data.Event)
	assert.Nil(t, event)
}
