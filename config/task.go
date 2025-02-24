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
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/TencentBlueKing/bkmonitor-datalink/pkg/libgse/beat"
	"github.com/TencentBlueKing/bkmonitor-datalink/pkg/libgse/logp"
	"github.com/elastic/beats/libbeat/common"
	"github.com/elastic/beats/libbeat/processors"

	"github.com/TencentBlueKing/bkunifylogbeat/utils"
)

// ConditionConfig : 用于条件表达式，目前支持=、!=、eq、neq、include、exclude、regex、nregex
type ConditionConfig struct {
	Index   int    `config:"index"`
	Key     string `config:"key"`
	Op      string `config:"op"`
	matcher MatchFunc
}

func (c *ConditionConfig) GetMatcher() MatchFunc {
	return c.matcher
}

// FilterConfig line filter config
type FilterConfig struct {
	Conditions []ConditionConfig `config:"conditions"`
}

type MountInfo struct {
	Source string `yaml:"source"`
	Dest   string `yaml:"dest"`
}

// ConditionSortByIndex condition配置
type ConditionSortByIndex []ConditionConfig

// Len is the number of elements in the collection.
func (a ConditionSortByIndex) Len() int { return len(a) }

// Swap swaps the elements with indexes i and j.
func (a ConditionSortByIndex) Swap(i, j int) { a[i], a[j] = a[j], a[i] }

// Less compare
func (a ConditionSortByIndex) Less(i, j int) bool { return a[i].Index < a[j].Index }

type ProcessorConfig struct {
	Processors processors.PluginConfig `config:"processors"`
}

type FiltersConfig struct {
	Delimiter string         `config:"delimiter"`
	Filters   []FilterConfig `config:"filters"`
	HasFilter bool
}

type SenderConfig struct {
	CanPackage   bool `config:"package"`
	PackageCount int  `config:"package_count"`

	// meta
	ExtMeta      map[string]interface{} `config:"ext_meta"`
	ExtMetaFiles []string               `config:"ext_meta_files"`
	ExtMetaEnv   map[string]string      `config:"ext_meta_env"`

	// Output
	RemovePathPrefix string      `config:"remove_path_prefix"` // 去除路径前缀
	MountInfos       []MountInfo `config:"mount_infos"`        // 挂载路径信息
	OutputFormat     string      `config:"output_format"`      // 输出格式，为了兼容老版采集器的输出格式
}

func metaKeyToField(key string) string {
	metaKey := strings.ReplaceAll(key, "/", "_")
	metaKey = strings.ReplaceAll(metaKey, ".", "_")
	return strings.ReplaceAll(metaKey, "-", "_")
}

func loadMetaFile(p string) map[string]string {
	b, err := os.ReadFile(p)
	if err != nil {
		return nil
	}

	meta := make(map[string]string)
	scanner := bufio.NewScanner(bytes.NewBuffer(b))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		v := strings.Trim(strings.TrimSpace(parts[1]), `"`)
		k := metaKeyToField(strings.TrimSpace(parts[0]))
		meta[k] = v
	}
	return meta
}

func (c SenderConfig) GetExtMeta() map[string]interface{} {
	ext := make(map[string]interface{})
	for k, v := range c.ExtMeta {
		ext[k] = v
	}

	for _, f := range c.ExtMetaFiles {
		for k, v := range loadMetaFile(f) {
			ext[k] = v
		}
	}

	for newKey, env := range c.ExtMetaEnv {
		ext[newKey] = os.Getenv(env)
	}

	return ext
}

// TaskConfig 采集任务配置
type TaskConfig struct {
	ID     string
	Type   string `config:"type"`
	DataID int    `config:"dataid"`

	ProcessorConfig `config:",inline"`
	FiltersConfig   `config:",inline"`
	SenderConfig    `config:",inline"`

	ext map[string]interface{}

	// 用来标识配置的唯一性
	InputID     string
	FilterID    string
	ProcessorID string
	SenderID    string

	IsContainerStd    bool `config:"is_container_std"`     // 是否为容器标准输出日志(docker)
	IsCRIContainerStd bool `config:"is_cri_container_std"` // 是否为容器标准输出日志(CRI)

	Output common.ConfigNamespace `config:"output"`

	RawConfig *beat.Config
}

func (c *TaskConfig) GetExtMeta() map[string]interface{} {
	return c.ext
}

// NewTaskConfig 创建采集任务配置
func NewTaskConfig(rawConfig *beat.Config) (*TaskConfig, error) {
	config := &TaskConfig{
		Type:   "log",
		DataID: 0,
		SenderConfig: SenderConfig{
			CanPackage:   true,
			PackageCount: 10,
			ExtMeta:      nil,
			OutputFormat: "v2",
		},
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
			// op must be "=" or "!=" or "include" or "exclude" or "eq" or "neq" or "regex" or "nregex"
			for i, condition := range f.Conditions {

				// 兼容旧数据 历史数据的字符串匹配包含 op 固定为 '='
				if condition.Index <= 0 && condition.Op == opEqual {
					condition.Op = opInclude
				}

				// 初始化条件匹配方法 Matcher
				matcher, err := getOperationFunc(condition.Op, condition.Key)

				if err != nil {
					return nil, fmt.Errorf("condition [%+v] init matcher error: %s", condition, err.Error())
				}

				condition.matcher = matcher

				// 重新赋值 condition
				f.Conditions[i] = condition
			}
			config.HasFilter = true
		}
	}

	// sort conditions
	if config.HasFilter {
		for _, f := range config.Filters {
			sort.Sort(ConditionSortByIndex(f.Conditions))
		}
	}

	//根据任务配置获取hash值
	err, config.ID = utils.HashRawConfig(config.RawConfig)
	if err != nil {
		return nil, err
	}
	config.ID = fmt.Sprintf("%d_%s", config.DataID, config.ID)

	initIDWithConfig(config)

	config.ext = config.SenderConfig.GetExtMeta() // 加载 extmeta
	return config, nil
}

func initIDWithConfig(config *TaskConfig) {
	var (
		hashVal    string
		copyConfig *common.Config
	)
	copyConfig, _ = common.NewConfigFrom(config.RawConfig)

	RemoveFields(copyConfig, map[string]interface{}{"dataid": config.DataID})
	_, hashVal = utils.HashRawConfig(copyConfig)
	config.SenderID = fmt.Sprintf("sender-%s", hashVal)

	RemoveFields(copyConfig, config.SenderConfig)
	_, hashVal = utils.HashRawConfig(copyConfig)
	config.ProcessorID = fmt.Sprintf("processor-%s", hashVal)

	RemoveFields(copyConfig, config.ProcessorConfig)
	RemoveFields(copyConfig, map[string]interface{}{"filters": config.Filters})
	_, hashVal = utils.HashRawConfig(copyConfig)
	config.FilterID = fmt.Sprintf("filter-%s", hashVal)

	RemoveFields(copyConfig, config.FiltersConfig)
	_, hashVal = utils.HashRawConfig(copyConfig)
	config.InputID = fmt.Sprintf("input-%s", hashVal)
}

func RemoveFields(config *common.Config, from interface{}) {
	removeConfig, _ := common.NewConfigFrom(from)
	for _, fieldName := range removeConfig.GetFields() {
		_, _ = config.Remove(fieldName, -1)
	}
}

// Same 用于采集器reload时，比较同一dataid的任务是否有做调整
func (sourceConfig *TaskConfig) Same(targetConfig *TaskConfig) bool {
	return sourceConfig.ID == targetConfig.ID
}

// loadTasks 从主配置中直接加载任务
func loadTasks(config Config) map[string]*TaskConfig {
	tasks := make(map[string]*TaskConfig)
	for _, c := range config.Tasks {
		cfg, err := common.NewConfigFrom(c)
		if err != nil {
			continue
		}
		task, err := NewTaskConfig(cfg)
		if err != nil {
			logp.L.Errorf("load task failed: %v", err)
			continue
		}

		tasks[task.ID] = task
	}
	return tasks
}

// GetTasks 根据主配置定义的从配置目录，获取采集器定义的任务列表
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

	// 合并主配置内容
	for k, v := range loadTasks(config) {
		tasks[k] = v
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

// CreateTaskConfig 根据字典生成任务配置
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
