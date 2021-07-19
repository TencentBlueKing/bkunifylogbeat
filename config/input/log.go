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
	"fmt"
	"time"

	cfg "github.com/TencentBlueKing/bkunifylogbeat/config"
	"github.com/TencentBlueKing/collector-go-sdk/v2/bkbeat/beat"
	"github.com/dustin/go-humanize"
)

//如果未配置close_inactive则直接默认为5分钟
type LogConfig struct {
	CloseInactive time.Duration `config:"close_inactive"`
	IgnoreOlder   time.Duration `config:"ignore_older"`
}

func init() {
	config := beat.MapStr{
		"enabled":         true,
		"scan_frequency":  10 * time.Second,
		"harvester_limit": 1000,
		"exclude_files":   []string{".gz$", ".bz2$", ".tgz$", ".tbz$", ".zip$", ".7z$", ".bak$", ".backup", ".swp$"},

		// close
		"close_inactive": 2 * time.Minute,

		// clean: 如果文件删除，则清除registry文件
		"clean_removed": true,

		// 监听文件变更时间
		"ignore_older": 168 * time.Hour,

		// harvester
		"tail_files": true,
		"encoding":   "utf-8",
		"symlinks":   true,
		"max_bytes":  200 * humanize.KByte,
	}
	err := cfg.Register("log", func(rawConfig *beat.Config) (*beat.Config, error) {
		var err error
		defaultConfig := beat.MapStr{}

		fields := rawConfig.GetFields()
		for key, value := range config {
			isExists := false
			for _, field := range fields {
				if key == field {
					isExists = true
					break
				}
			}
			if !isExists {
				defaultConfig[string(key)] = value
			}
		}

		// 特殊配置处理
		logConfig := &LogConfig{
			CloseInactive: 5 * time.Minute,
			IgnoreOlder:   168 * time.Hour,
		}
		err = rawConfig.Unpack(&logConfig)
		if err != nil {
			return nil, fmt.Errorf("error parsing raw config => %v", err)
		}
		// 1. FD释放（close_inactive）配置不能超过5分钟
		if logConfig.CloseInactive > 5*time.Minute {
			defaultConfig["close_inactive"] = 5 * time.Minute
		}
		// 2. CleanInactive 必须大于 IgnoreOlder + scan_frequency
		defaultConfig["clean_inactive"] = logConfig.IgnoreOlder + 1*time.Hour

		err = rawConfig.Merge(defaultConfig)
		if err != nil {
			return nil, err
		}
		return rawConfig, nil
	})
	if err != nil {
		panic(err)
	}
}
