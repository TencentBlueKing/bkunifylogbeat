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

package utils

import (
	"testing"

	"github.com/elastic/beats/libbeat/common"
	"github.com/stretchr/testify/assert"
)

// TestMd5 测试字符串MD5
func TestMd5(t *testing.T) {
	tests := []struct {
		source string
		target string
	}{
		{
			source: "123456",
			target: "e10adc3949ba59abbe56e057f20f883e",
		},
	}

	for _, test := range tests {
		assert.Equal(t, Md5(test.source), test.target)
	}
}

// TestHashRawConfigHash 测试RawConfig HASH结果
func TestHashRawConfigHash(t *testing.T) {
	//RawConfig不依赖于各配置的顺序
	vars := map[string]interface{}{
		"dataid":          "999990001",
		"harvester_limit": 10,
	}
	rawConfig, _ := common.NewConfigFrom(vars)
	_, rawConfigHash1 := HashRawConfig(rawConfig)

	vars2 := map[string]interface{}{
		"harvester_limit": 10,
		"dataid":          "999990001",
	}
	rawConfig2, _ := common.NewConfigFrom(vars2)
	_, rawConfigHash2 := HashRawConfig(rawConfig2)

	assert.Equal(t, rawConfigHash1, rawConfigHash2)

	//差异值：类型不同hash结果也不同
	vars3 := map[string]interface{}{
		"harvester_limit": "10",
		"dataid":          "999990001",
	}
	rawConfig3, _ := common.NewConfigFrom(vars3)
	_, rawConfigHash3 := HashRawConfig(rawConfig3)

	assert.NotEqual(t, rawConfigHash1, rawConfigHash3)
}
