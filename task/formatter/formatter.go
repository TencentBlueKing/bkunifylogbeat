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

package formatter

import (
	"fmt"

	"github.com/TencentBlueKing/bkunifylogbeat/config"
	"github.com/TencentBlueKing/collector-go-sdk/v2/bkbeat/beat"
	"github.com/elastic/beats/filebeat/util"
)

// Formatter: 采集器事件包格式化接口, 根据任务配置返回相应的格式
type Formatter interface {
	Format([]*util.Data) beat.MapStr
}

// FormatterFactory is used by output plugins to build an output instance
type FormatterFactory = func(config *config.TaskConfig) (Formatter, error)

// FindFormatterFactory: 获取格式化器实例
func FindFormatterFactory(name string) (FormatterFactory, error) {
	f, exist := formatterRegistry[name]
	if !exist {
		return nil, fmt.Errorf("formatter is not exists")
	}
	return f, nil
}

var formatterRegistry = make(map[string]FormatterFactory)

// FormatterRegister: 注册sender输出方法
func FormatterRegister(name string, factory FormatterFactory) error {
	if name == "" {
		return fmt.Errorf("error registering input config: name cannot be empty")
	}
	if factory == nil {
		return fmt.Errorf("error registering input config '%v': config cannot be empty", name)
	}
	if _, exists := formatterRegistry[name]; exists {
		return fmt.Errorf("error registering input config '%v': already registered", name)
	}

	formatterRegistry[name] = factory
	return nil
}
