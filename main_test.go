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

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/TencentBlueKing/bkunifylogbeat/beater"
	"github.com/TencentBlueKing/collector-go-sdk/v2/bkbeat/beat"
	"github.com/elastic/beats/libbeat/cmd/instance"
	"github.com/elastic/beats/libbeat/publisher/processing"
	"github.com/stretchr/testify/assert"
)

func TestLogBeat(t *testing.T) {
	absPath, err := filepath.Abs("./tests/conf/")
	assert.NotNil(t, absPath)
	assert.NoError(t, err)
	configFile := absPath + "/main.conf"
	os.Args = []string{"cmd", "-c", configFile}

	//step 1: 初始化采集器
	settings := instance.Settings{
		Processing: processing.MakeDefaultSupport(false),
	}
	config, err := beat.InitWithPublishConfig(beatName, version, beat.PublishConfig{
		PublishMode: beat.GuaranteedSend,
		ACKEvents:   beater.AckEvents,
	}, settings)
	if err != nil {
		fmt.Printf("Init filed with error: %s\n", err.Error())
		os.Exit(1)
	}

	// step 2：加载配置
	bt, err := beater.New(config)
	if err != nil {
		fmt.Printf("New failed with error: %s\n\n", err.Error())
		os.Exit(1)
	}
	// step 3：主动开启采集器
	go func() {
		_, err = os.Stat("/data1/bkunifylogbeat/conf/task.conf")
		if err != nil {
			close(beat.Done)
		}
	}()

	error := bt.Run()
	assert.NoError(t, error)
}
