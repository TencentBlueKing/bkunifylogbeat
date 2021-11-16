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
	"fmt"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/TencentBlueKing/bkunifylogbeat/utils"
	"github.com/TencentBlueKing/collector-go-sdk/v2/bkbeat/beat"
	"github.com/TencentBlueKing/collector-go-sdk/v2/bkbeat/logp"
	"github.com/elastic/beats/libbeat/common"
	"github.com/elastic/beats/libbeat/processors"
)

// ConditionConfig: 用于条件表达式，目前支持=、!=
type ConditionConfig struct {
	Index int    `config:"index"`
	Key   string `config:"key"`
	Op    string `config:"op"`
}

// FilterConfig line filter config
type FilterConfig struct {
	Conditions []ConditionConfig `config:"conditions"`
}

//condition配置
type ConditionSortByIndex []ConditionConfig

// Len is the number of elements in the collection.
func (a ConditionSortByIndex) Len() int { return len(a) }

// Swap swaps the elements with indexes i and j.
func (a ConditionSortByIndex) Swap(i, j int) { a[i], a[j] = a[j], a[i] }

// Len is the number of elements in the collection.
func (a ConditionSortByIndex) Less(i, j int) bool { return a[i].Index < a[j].Index }

// 采集任务配置
type TaskConfig struct {
	ID     string
	Type   string `config:"type"`
	DataID int    `config:"dataid"`
	// Processor
	Processors processors.PluginConfig `config:"processors"`
	Delimiter  string                  `config:"delimiter"`
	Filters    []FilterConfig          `config:"filters"`
	HasFilter  bool
	// Sender
	CanPackage   bool        `config:"package"`
	PackageCount int         `config:"package_count"`
	ExtMeta      interface{} `config:"ext_meta"`

	// Output
	RemovePathPrefix string `config:"remove_path_prefix"` // 去除路径前缀
	IsContainerStd   bool   `config:"is_container_std"`   // 是否为容器标准输出日志
	OutputFormat     string `config:"output_format"`      // 输出格式，为了兼容老版采集器的输出格式

	RawConfig *beat.Config
}

// 创建采集任务配置
func NewTaskConfig(rawConfig *beat.Config) (*TaskConfig, error) {
	config := &TaskConfig{
		Type:         "log",
		DataID:       0,
		CanPackage:   true,
		PackageCount: 10,
		ExtMeta:      nil,
		OutputFormat: "v2",
	}
	err := rawConfig.Unpack(&config)
	if err != nil {
		return nil, fmt.Errorf("error parsing raw config => %v", err)
	}
	if config.DataID == 0 {
		return nil, fmt.Errorf("error creating task, DataID cannot be empty")
	}

	config.RawConfig, err = initTaskConfig(config.Type, rawConfig)
	if err != nil {
		return nil, fmt.Errorf("error init config: %v", err)
	}

	// Filter
	config.HasFilter = false
	if len(config.Delimiter) == 1 {
		for _, f := range config.Filters {
			// op must be "=" or "!="
			for _, condition := range f.Conditions {
				if condition.Op != "=" && condition.Op != "!=" {
					return nil, fmt.Errorf("op must = or !=")
				}
			}
			config.HasFilter = true
		}
	}

	// sort conditions
	if config.HasFilter {
		for _, f := range config.Filters {
			sort.Sort(ConditionSortByIndex(f.Conditions))
			// uniq filter index
			lastIndex := 0
			for _, condition := range f.Conditions {
				if lastIndex == condition.Index {
					return nil, fmt.Errorf("filter has duplicate index")
				}
				lastIndex = condition.Index
			}
		}
	}
	//根据任务配置获取hash值
	err, config.ID = utils.HashRawConfig(config.RawConfig)
	if err != nil {
		return nil, err
	}
	config.ID = fmt.Sprintf("%s_%s", strconv.Itoa(config.DataID), config.ID)

	return config, nil
}

// Same用于采集器reload时，比较同一dataid的任务是否有做调整
func (sourceConfig *TaskConfig) Same(targetConfig *TaskConfig) bool {
	return sourceConfig.ID == targetConfig.ID
}

// GetTasks根据主配置定义的从配置目录，获取采集器定义的任务列表
func GetTasks(config Config) map[string]*TaskConfig {
	tasks := make(map[string]*TaskConfig)

	for _, v := range config.SecConfigs {
		pattern := v.Path + "/" + v.Pattern
		matches, err := filepath.Glob(pattern)
		if err != nil {
			logp.L.Errorf("secondary config pattern %s format error with msg: %v", pattern, err)
			continue
		}
		if len(matches) == 0 {
			logp.L.Warnf("No config file found with pattern %s.", pattern)
			continue
		}
		for _, path := range matches {
			logp.L.Debugf("logbeat", "found secondary config file: %s", path)
			// load secondary config
			secConfRaw, err := beat.LoadFile(path)
			if err != nil {
				logp.L.Errorf("secondary config file %s format error with msg: %v", path, err)
				continue
			}
			// get local configs
			localConfigsRaw, err := secConfRaw.Child("local", -1)
			if err != nil {
				logp.L.Errorf("Error reading config file: %v", err)
				continue
			}
			n, err := localConfigsRaw.CountField("")
			if err != nil {
				logp.L.Errorf("Error reading config file: %v", err)
				continue
			}
			for i := 0; i < n; i++ {
				localConfigRaw, err := localConfigsRaw.Child("", i)
				if err != nil {
					logp.L.Errorf("Error reading config file: %v", err)
					continue
				}
				task, err := NewTaskConfig(localConfigRaw)
				if err != nil {
					logp.L.Errorf("Error reading config file: %v", err)
					continue
				}
				tasks[task.ID] = task
			}
		}
	}
	return tasks
}

// initTaskConfig: 任务配置初始化
func initTaskConfig(inputType string, rawConfig *beat.Config) (*beat.Config, error) {
	f, exist := registry[inputType]
	if !exist {
		return rawConfig, nil
	}
	return f(rawConfig)
}

//CreateTaskConfig: 根据字典生成任务配置
func CreateTaskConfig(vars map[string]interface{}) (*TaskConfig, error) {
	rawConfig, err := common.NewConfigFrom(vars)
	if err != nil {
		return nil, err
	}
	config, err := NewTaskConfig(rawConfig)
	if err != nil {
		return nil, err
	}
	return config, nil
}
