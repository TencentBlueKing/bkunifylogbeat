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

package input

import (
	"testing"

	cfg "github.com/TencentBlueKing/bkunifylogbeat/config"
	"github.com/TencentBlueKing/collector-go-sdk/v2/bkbeat/logp"
	"github.com/elastic/beats/libbeat/common"
	libbeatlogp "github.com/elastic/beats/libbeat/logp"
	"github.com/stretchr/testify/assert"
)

func init() {
	logp.SetLogger(libbeatlogp.L())
}

func mockTaskConfig(vars map[string]interface{}) (*cfg.TaskConfig, error) {
	rawConfig, err := common.NewConfigFrom(vars)
	if err != nil {
		return nil, err
	}
	config, err := cfg.NewTaskConfig(rawConfig)
	if err != nil {
		return nil, err
	}
	return config, nil
}

// 测试日志采集配置
func TestNewTaskConfig(t *testing.T) {
	var err error
	var config *cfg.TaskConfig

	vars := map[string]interface{}{
		"dataid":        "999990001",
		"package":       true,
		"package_count": 10,
	}

	// 创建任务配置
	config, err = mockTaskConfig(vars)
	if err != nil {
		assert.Nil(t, err)
		return
	}

	// 日志采集默认配置
	taskConfig := map[string]interface{}{}
	config.RawConfig.Unpack(taskConfig)
	assert.Equal(t, taskConfig["tail_files"].(bool), true)
	assert.Equal(t, taskConfig["close_inactive"].(string), "2m0s")
	assert.Equal(t, taskConfig["ignore_older"].(string), "168h0m0s")
	assert.Equal(t, taskConfig["clean_inactive"].(string), "169h0m0s")

	//case 1: 变更close_inactive
	vars["close_inactive"] = "86400s"

	config, err = mockTaskConfig(vars)
	if err != nil {
		assert.Nil(t, err)
		return
	}
	taskConfig = map[string]interface{}{}
	config.RawConfig.Unpack(taskConfig)
	assert.Equal(t, taskConfig["tail_files"].(bool), true)
	assert.Equal(t, taskConfig["close_inactive"].(string), "5m0s")

	//case 2: 变更close_inactive 2
	vars["close_inactive"] = "60s"

	config, err = mockTaskConfig(vars)
	if err != nil {
		assert.Nil(t, err)
		return
	}
	taskConfig = map[string]interface{}{}
	config.RawConfig.Unpack(taskConfig)
	assert.Equal(t, taskConfig["tail_files"].(bool), true)
	assert.Equal(t, taskConfig["close_inactive"].(string), "60s")

	//case 3: 修改ignore_older时间
	vars["ignore_older"] = "2678400s"

	config, err = mockTaskConfig(vars)
	if err != nil {
		assert.Nil(t, err)
		return
	}
	taskConfig = map[string]interface{}{}
	config.RawConfig.Unpack(taskConfig)
	assert.Equal(t, taskConfig["ignore_older"].(string), "2678400s")
	assert.Equal(t, taskConfig["clean_inactive"].(string), "745h0m0s")

}
