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
	"math"
	"time"

	"github.com/TencentBlueKing/bkmonitor-datalink/pkg/libgse/beat"
	"github.com/TencentBlueKing/bkmonitor-datalink/pkg/libgse/logp"
)

// 主配置
type Config struct {
	// Max bytes for  buffer
	MaxBytes int `config:"maxbytes"`
	// max line for buffer
	Maxline int `config:"maxline"`
	// timeout for buffer
	BufferTimeout time.Duration `config:"buffertimeout"`
	// max cpu limit percent
	MaxCpuLimit int `config:"max_cpu_limit"` // 最大CPU限制，仅在某些极端情况下开启
	// CpuCheckTimes
	CpuCheckTimes int `config:"cpu_check_times"` // 1秒内检测多少次CPU, 可选值，[1-10]
	// AdaptiveScan 动态扫描周期配置
	AdaptiveScan AdaptiveScanConfig `config:"adaptive_scan"`

	// SecConfigs sec config path and pattern
	SecConfigs []SecConfigItem `config:"multi_config"`
	Seccomp    Seccomp         `config:"seccomp"`

	// Tasks 允许加载子配置采集项
	Tasks []interface{} `config:"tasks"`

	Registry Registry `config:"registry"`

	HostIDPath         string `config:"host_id_path"`
	CmdbLevelMaxLength int    `config:"cmdb_level_max_length"`
	IgnoreCmdbLevel    bool   `config:"ignore_cmdb_level"`
	MustHostIDExist    bool   `config:"must_host_id_exist"`
	CheckDiff          bool   `config:"check_diff"`
	WindowsReloadPath  string `config:"windows_reload_path"`

	// 采集状态的唯一标识符
	FileIdentifier string `config:"file_identifier"`
}

// AdaptiveScanConfig 控制扫描周期自适应及其全局 CPU 预算。
type AdaptiveScanConfig struct {
	Enabled bool `config:"enabled"`
	// MinScanFrequency 是动态缩短后的下限；若原配置周期更短，仍以原配置为准。
	MinScanFrequency time.Duration `config:"min_scan_frequency"`
	// ScanCPUPercent 是所有 input 聚合扫描墙钟耗时占单核时间的目标比例，不是进程 CPU 使用率；
	// 当容器 CPU quota 小于 1 核时，会按 quota 等比例缩小预算。
	ScanCPUPercent float64 `config:"scan_cpu_percent"`
	// ControlInterval 是 governor 的采样与调节周期，与文件扫描周期相互独立。
	ControlInterval time.Duration `config:"control_interval"`
}

func (c AdaptiveScanConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	if c.MinScanFrequency <= 0 {
		return fmt.Errorf("adaptive_scan.min_scan_frequency must be greater than 0")
	}
	if c.ScanCPUPercent <= 0 || c.ScanCPUPercent > 100 ||
		math.IsNaN(c.ScanCPUPercent) || math.IsInf(c.ScanCPUPercent, 0) {
		return fmt.Errorf("adaptive_scan.scan_cpu_percent must be greater than 0 and no more than 100")
	}
	if c.ControlInterval <= 0 {
		return fmt.Errorf("adaptive_scan.control_interval must be greater than 0")
	}
	return nil
}

// 从配置目录
type SecConfigItem struct {
	Path    string `config:"path"`
	Pattern string `config:"file_pattern"`
}

// 系统调用配置
type Seccomp struct {
	Enable bool `config:"enable"`
}

// 采集状态
type Registry struct {
	FlushTimeout time.Duration `config:"flush"`
	GcFrequency  time.Duration `config:"gc_frequency"`
}

// Factory 默认配置
type Factory = func(rawConfig *beat.Config) (*beat.Config, error)

var registry = make(map[string]Factory)

// Register 用于处理采集任务配置兼容
func Register(name string, factory Factory) error {
	if name == "" {
		return fmt.Errorf("error registering input config: name cannot be empty")
	}
	if factory == nil {
		return fmt.Errorf("error registering input config '%v': config cannot be empty", name)
	}
	if _, exists := registry[name]; exists {
		return fmt.Errorf("error registering input config '%v': already registered", name)
	}

	registry[name] = factory
	return nil
}

// Parse用于主配置解析
func Parse(cfg *beat.Config) (Config, error) {
	config := Config{
		MaxBytes:      1024 * 512,
		Maxline:       10,
		BufferTimeout: 1,
		MaxCpuLimit:   -1,
		CpuCheckTimes: 10,
		// 默认开启，并使用 1 秒最小周期和 5% 单核预算作为保守起步参数。
		AdaptiveScan: AdaptiveScanConfig{
			Enabled:          true,
			MinScanFrequency: time.Second,
			ScanCPUPercent:   5,
			ControlInterval:  3 * time.Second,
		},
		Registry: Registry{
			FlushTimeout: 1 * time.Second,
			GcFrequency:  1 * time.Minute,
		},
		Seccomp: Seccomp{
			Enable: false,
		},
		FileIdentifier: "inode",
	}
	err := cfg.Unpack(&config)
	if err != nil {
		return config, fmt.Errorf("unpack config error, %v", err)
	}
	if err = config.AdaptiveScan.Validate(); err != nil {
		return config, err
	}
	logp.L.Infof("load config: %+v", config)

	return config, nil
}
