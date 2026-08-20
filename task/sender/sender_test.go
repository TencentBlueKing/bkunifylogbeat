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

package sender

import (
	"testing"
	"time"

	"github.com/TencentBlueKing/bkmonitor-datalink/pkg/libgse/beat"
	"github.com/TencentBlueKing/bkmonitor-datalink/pkg/libgse/logp"
	"github.com/elastic/beats/filebeat/util"
	"github.com/elastic/beats/libbeat/common"
	libbeatlogp "github.com/elastic/beats/libbeat/logp"
	"github.com/stretchr/testify/assert"

	"github.com/TencentBlueKing/bkunifylogbeat/config"
	"github.com/TencentBlueKing/bkunifylogbeat/task/formatter"
	"github.com/TencentBlueKing/bkunifylogbeat/tests"

	cfg "github.com/TencentBlueKing/bkunifylogbeat/config"
)

func init() {
	logp.SetLogger(libbeatlogp.L())
	initMockFormatter()
}

var (
	sendNums     int
	fileSource1  = "/tmp/test1.log"
	fileSource2  = "/tmp/test2.log"
	fileText     = "test"
	fileTextNull = ""
	packageCount = 10
	outputFormat = "mock_v2"

	event beat.Event
)

type mockFormatter struct {
	taskConfig *config.TaskConfig
}

// NewMockFormatter: mock formatter
func NewMockFormatter(config *config.TaskConfig) (*mockFormatter, error) {
	f := &mockFormatter{
		taskConfig: config,
	}
	return f, nil
}

func (f mockFormatter) Format(events []*util.Data) beat.MapStr {
	lastState := events[len(events)-1].GetState()
	return beat.MapStr{
		"dataid":   f.taskConfig.DataID,
		"filename": lastState.Source,
	}
}

func initMockFormatter() {
	err := formatter.FormatterRegister(outputFormat, func(config *config.TaskConfig) (formatter.Formatter, error) {
		return NewMockFormatter(config)
	})
	if err != nil {
		panic(err)
	}
}

func mockSender(canPackage bool, packageCount int) (*Sender, error) {
	var err error
	vars, err := common.NewConfigFrom(map[string]interface{}{
		"dataid":        "999990001",
		"output_format": outputFormat,
		"package":       canPackage,
		"package_count": packageCount,
	})
	if err != nil {
		return nil, err
	}
	taskConfig, err := config.NewTaskConfig(cfg.Config{}, vars)
	if err != nil {
		return nil, err
	}

	taskNode := tests.MockTaskNode(taskConfig)
	go func() {
		for {
			e := <-taskNode.In
			event = e.(beat.Event)
			sendNums++
		}
	}()

	sender, err := NewSender(taskConfig, taskNode)
	if err != nil {
		return nil, err
	}

	return sender, nil
}

// TestSend 测试打包发送
func TestSend(t *testing.T) {
	var sender *Sender
	var err error

	sender, err = mockSender(false, packageCount)
	if err != nil {
		panic(err)
		return
	}

	// Send
	sendNums = 0
	sender.In <- tests.MockLogEvent(fileSource1, fileText)
	sender.In <- tests.MockLogEvent(fileSource1, fileText)
	sender.In <- tests.MockLogEvent(fileSource2, fileText)

	time.Sleep(500 * time.Millisecond)
	assert.Equal(t, sendNums, 3)
	assert.NotNil(t, event.Fields)

	// Package send
	sender, err = mockSender(true, packageCount)
	if err != nil {
		panic(err)
		return
	}
	sendNums = 0
	sender.In <- tests.MockLogEvent(fileSource1, fileText)
	sender.In <- tests.MockLogEvent(fileSource1, fileText)
	sender.In <- tests.MockLogEvent(fileSource1, fileText)
	time.Sleep(1200 * time.Millisecond)
	assert.Equal(t, sendNums, 1)

	// Package send: diff file
	sendNums = 0
	sender.In <- tests.MockLogEvent(fileSource1, fileText)
	sender.In <- tests.MockLogEvent(fileSource2, fileText)
	sender.In <- tests.MockLogEvent(fileSource1, fileText)
	time.Sleep(1200 * time.Millisecond)
	assert.Equal(t, sendNums, 2)

	// Filter event
	sendNums = 0
	// No.1 event
	sender.In <- tests.MockLogEvent(fileSource1, fileTextNull)
	// No.2 event
	sender.In <- tests.MockLogEvent(fileSource1, fileText)
	sender.In <- tests.MockLogEvent(fileSource1, fileText)
	sender.In <- tests.MockLogEvent(fileSource1, fileTextNull)
	// No.3 event
	sender.In <- tests.MockLogEvent(fileSource1, fileTextNull)
	// No.4 event
	sender.In <- tests.MockLogEvent(fileSource1, fileTextNull)
	// No.5 event
	sender.In <- tests.MockLogEvent(fileSource1, fileText)
	sender.In <- tests.MockLogEvent(fileSource1, fileText)
	time.Sleep(1200 * time.Millisecond)
	assert.Equal(t, sendNums, 5)

	// Package Count
	sender, err = mockSender(true, 2)
	if err != nil {
		panic(err)
		return
	}
	sendNums = 0
	for i := 0; i < 8; i++ {
		sender.In <- tests.MockLogEvent(fileSource1, fileText)
	}
	time.Sleep(200 * time.Millisecond)
	assert.Equal(t, sendNums, 4)
}
