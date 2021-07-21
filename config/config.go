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
	"time"

	"github.com/TencentBlueKing/collector-go-sdk/v2/bkbeat/beat"
	"github.com/TencentBlueKing/collector-go-sdk/v2/bkbeat/logp"
)

// 主配置
type Config struct {
	// Max bytes for  buffer
	MaxBytes int `config:"maxbytes"`
	// max line for buffer
	Maxline int `config:"maxline"`
	// timeout for buffer
	BufferTimeout time.Duration `config:"buffertimeout"`

	// SecConfigs sec config path and pattern
	SecConfigs []SecConfigItem `config:"multi_config"`

	Registry Registry `config:"registry"`
}

// 从配置目录
type SecConfigItem struct {
	Path    string `config:"path"`
	Pattern string `config:"file_pattern"`
}

// 采集状态
type Registry struct {
	FlushTimeout time.Duration `config:"flush"`
	GcFrequency  time.Duration `config:"gc_frequency"`
}

//默认配置
type Factory = func(rawConfig *beat.Config) (*beat.Config, error)

var registry = make(map[string]Factory)

const (
	LOG_INPUT  = "log"
	UDP_INPUT  = "udp"
	OTLP_INPUT = "otlp"
)

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
		Registry: Registry{
			FlushTimeout: 1 * time.Second,
			GcFrequency:  1 * time.Minute,
		},
	}
	err := cfg.Unpack(&config)
	if err != nil {
		return config, fmt.Errorf("unpack config error, %v", err)
	}
	logp.L.Infof("load config: %+v", config)

	return config, nil
}
