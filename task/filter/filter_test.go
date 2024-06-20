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
	"github.com/TencentBlueKing/bkmonitor-datalink/pkg/libgse/beat"
	"github.com/TencentBlueKing/bkmonitor-datalink/pkg/libgse/logp"
	libbeatlogp "github.com/elastic/beats/libbeat/logp"
	"testing"
	"time"

	cfg "github.com/TencentBlueKing/bkunifylogbeat/config"
	"github.com/TencentBlueKing/bkunifylogbeat/tests"
)

var (
	event beat.Event
)

func init() {
	logp.SetLogger(libbeatlogp.L())
}

func newFilter(taskFilterConfig map[string]interface{}) *Filters {
	config, err := cfg.CreateTaskConfig(taskFilterConfig)
	if err != nil {
		panic(err)
	}

	taskNode := tests.MockTaskNode(config)
	go func() {
		for {
			e := <-taskNode.In
			event = e.(beat.Event)
		}
	}()

	filter, _ := NewFilters(config, taskNode)
	return filter
}

// TestFilter 测试原过滤器兼容
func TestNewFilters(t *testing.T) {
	event = beat.Event{}
	filter := newFilter(map[string]interface{}{
		"dataid":    "999990001",
		"delimiter": "|",
		"filters": []cfg.FilterConfig{
			{
				Conditions: []cfg.ConditionConfig{
					{
						Index: -1,
						Key:   "test",
						Op:    "=",
					},
				},
			},
		},
	})

	if filter == nil {
		t.Error("new filter error, not nil")
	}
}

func TestFilters_Handle(t *testing.T) {
	event = beat.Event{}
	filter := newFilter(map[string]interface{}{
		"dataid":    "999990001",
		"delimiter": "|",
		"filters": []cfg.FilterConfig{
			{
				Conditions: []cfg.ConditionConfig{
					{
						Index: -1,
						Key:   "test",
						Op:    "=",
					},
				},
			},
		},
	})

	// not match
	data := tests.MockLogEvent("/test.log", "not match log text")
	filter.In <- data
	time.Sleep(2 * time.Second)

	if event.Fields != nil {
		t.Error("filter must not match.")
		return
	}

	// match
	data = tests.MockLogEvent("/test.log", "test")
	filter.In <- data
	time.Sleep(2 * time.Second)

	if event.Fields == nil {
		t.Error("filter error. not effect")
		return
	}
}

func TestFilters_Handle_Multi_Condition(t *testing.T) {
	event = beat.Event{}
	filter := newFilter(map[string]interface{}{
		"dataid":    "999990001",
		"delimiter": "|",
		"filters": []cfg.FilterConfig{
			{
				Conditions: []cfg.ConditionConfig{
					{
						Index: 1,
						Key:   "debug",
						Op:    "!=",
					},
					{
						Index: 2,
						Key:   "test",
						Op:    "=",
					},
				},
			},
		},
	})

	// not match condition
	data := tests.MockLogEvent("/test.log", "debug|test")
	filter.In <- data
	time.Sleep(2 * time.Second)
	if event.Fields != nil {
		t.Error("filter error. not effect")
		return
	}

	// match condition
	data = tests.MockLogEvent("/test.log", "info|test")
	filter.In <- data
	time.Sleep(2 * time.Second)
	if event.Fields == nil {
		t.Error("filter error. not effect")
		return
	}
}

func TestFilters_Handle_Include(t *testing.T) {
	event = beat.Event{}
	filter := newFilter(map[string]interface{}{
		"dataid":    "999990001",
		"delimiter": "|",
		"filters": []cfg.FilterConfig{
			{
				Conditions: []cfg.ConditionConfig{
					{
						Index: -1,
						Key:   "test",
						Op:    "include",
					},
				},
			},
		},
	})

	// not match
	data := tests.MockLogEvent("/test.log", "not match log text")
	filter.In <- data
	time.Sleep(2 * time.Second)

	if event.Fields != nil {
		t.Error("filter must not match.")
		return
	}

	// match
	data = tests.MockLogEvent("/test.log", "test include")
	filter.In <- data
	time.Sleep(2 * time.Second)

	if event.Fields == nil {
		t.Error("filter error. not effect")
		return
	}
}

func TestFilters_Handle_Regex(t *testing.T) {
	event = beat.Event{}
	filter := newFilter(map[string]interface{}{
		"dataid":    "999990001",
		"delimiter": "|",
		"filters": []cfg.FilterConfig{
			{
				Conditions: []cfg.ConditionConfig{
					{
						Index: -1,
						Key:   ".*info.*",
						Op:    "regex",
					},
				},
			},
		},
	})

	// not match
	data := tests.MockLogEvent("/test.log", "test regex not matching")
	filter.In <- data
	time.Sleep(2 * time.Second)

	if event.Fields != nil {
		t.Error("filter must not match.")
		return
	}

	// match
	data = tests.MockLogEvent("/test.log", "test regex matching info log ending")
	filter.In <- data
	time.Sleep(2 * time.Second)

	if event.Fields == nil {
		t.Error("filter error. not effect")
		return
	}
}
