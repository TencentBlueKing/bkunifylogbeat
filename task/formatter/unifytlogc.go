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

// 兼容bklogbeat格式

package formatter

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/TencentBlueKing/bkunifylogbeat/config"
	"github.com/TencentBlueKing/bkunifylogbeat/utils"
	"github.com/TencentBlueKing/collector-go-sdk/v2/bkbeat/beat"
	"github.com/elastic/beats/filebeat/util"
	"github.com/golang/groupcache/lru"
)

//如果未配置close_inactive则直接默认为5分钟
type LogConfig struct {
	HarvesterLimit int `config:"harvester_limit"`
}

type unifytlogcFormatter struct {
	taskConfig *config.TaskConfig
	// cache 用于存储unifytlogc对日志路径的解析结果
	cache *lru.Cache
}

//NewUnifytlogcFormatter: 兼容unifytlogc输出格式
func NewUnifytlogcFormatter(config *config.TaskConfig) (*unifytlogcFormatter, error) {
	//获取任务配置中最大的FD数量
	logConfig := &LogConfig{
		HarvesterLimit: 1000,
	}
	err := config.RawConfig.Unpack(&logConfig)
	if err != nil {
		return nil, fmt.Errorf("error parsing raw config => %v", err)
	}

	f := &unifytlogcFormatter{
		taskConfig: config,
		cache:      lru.New(logConfig.HarvesterLimit),
	}
	return f, nil
}

//Format: unifytlogc输出格式兼容
func (f unifytlogcFormatter) Format(events []*util.Data) beat.MapStr {
	var (
		datetime, utcTime string
		timestamp         int64
	)
	datetime, utcTime, timestamp = utils.GetDateTime()

	lastState := events[len(events)-1].GetState()
	data := beat.MapStr{
		"_bizid_":     0,
		"_errorcode_": 0,
		"_type_":      0,
		"_worldid_":   -1,
		"_dstdataid_": f.taskConfig.DataID,
		"_srcdataid_": f.taskConfig.DataID,
		"_path_":      lastState.Source,
		"_time_":      datetime,
		"_utctime_":   utcTime,
		"dataid":      f.taskConfig.DataID,
		"time":        timestamp,
	}

	hasEvent := false

	var texts []string
	for _, event := range events {
		item := event.Event.Fields
		if item == nil {
			continue
		}
		hasEvent = true
		texts = append(texts, item["data"].(string))
	}

	// 仅需要更新采集状态的事件数
	if !hasEvent {
		return nil
	}
	data["_value_"] = texts
	//获取worldID
	data["_worldid_"] = f.getWorldID(lastState.Source)

	//发送正常事件
	if f.taskConfig.ExtMeta != nil {
		data["_private_"] = f.taskConfig.ExtMeta
	} else {
		data["_private_"] = ""
	}
	return data
}

func (f unifytlogcFormatter) getWorldID(path string) int64 {
	cache, ok := f.cache.Get(path)
	if ok {
		return cache.(int64)
	}

	// 如果filename所在目录是“xxx_数字”的形式，worldid就是这个数字，否则为-1
	worldID := int64(-1)
	dir, _ := filepath.Split(path)
	baseName := filepath.Base(dir)

	separator := "_"
	strPos := strings.Index(baseName, separator)

	if strPos <= 0 || strings.Count(baseName, separator) != 1 {
		f.cache.Add(path, worldID)
		return worldID
	}

	candidate := baseName[strPos+1:]
	worldID, err := strconv.ParseInt(candidate, 10, 64)
	if err != nil {
		worldID = int64(-1)
		f.cache.Add(path, worldID)
		return worldID
	}
	f.cache.Add(path, worldID)
	return worldID
}

func init() {
	err := FormatterRegister("unifytlogc", func(config *config.TaskConfig) (Formatter, error) {
		return NewUnifytlogcFormatter(config)
	})
	if err != nil {
		panic(err)
	}
}
