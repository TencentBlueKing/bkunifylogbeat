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
	"github.com/TencentBlueKing/bkmonitor-datalink/pkg/libgse/monitoring"
	"github.com/TencentBlueKing/bkmonitor-datalink/pkg/libgse/output/gse"
	"os"

	"github.com/TencentBlueKing/bkmonitor-datalink/pkg/libgse/beat"
	"github.com/elastic/beats/libbeat/cmd/instance"
	"github.com/elastic/beats/libbeat/publisher/processing"

	"github.com/TencentBlueKing/bkunifylogbeat/beater"
	_ "github.com/TencentBlueKing/bkunifylogbeat/json"
)

var (
	beatName = "bkunifylogbeat"
	version  = "7.2.1"
)

func initGseHook() {
	gse.RegisterSendHook(func(i int32, f float64) bool {
		monitoring.NewFloatWithDataID(int(i), "beat_send_bytes_total").Add(f)
		return true
	})
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "%s failed with error: %s\n", beatName, err)
		os.Exit(1)
	}
}

// run 启动采集器并返回运行错误。
// 框架资源（PID 锁与 Storage）不归 LogBeat 所有，统一由 beat.Stop 释放；
// 放在 defer 中保证初始化、创建、运行的所有返回路径都先完成清理，再由 main 决定退出码，
// 避免在清理前 os.Exit。beat.Stop 在未运行或已停止时返回 "no beat running"，
// 该情况下的清理本身是安全的，且不能覆盖原始错误，因此忽略其返回值。
func run() error {
	defer func() {
		_ = beat.Stop()
	}()

	//step 1: 初始化采集器
	settings := instance.Settings{
		Processing: processing.MakeDefaultSupport(false),
	}
	config, err := beat.InitWithPublishConfig(beatName, version, beat.PublishConfig{
		PublishMode: beat.GuaranteedSend,
		ACKEvents:   beater.AckEvents,
	}, settings)
	if err != nil {
		return fmt.Errorf("init %s: %w", beatName, err)
	}

	// step 2：加载配置
	bt, err := beater.New(config)
	if err != nil {
		return fmt.Errorf("create %s beater: %w", beatName, err)
	}
	initGseHook()
	// step 3：主动开启采集器
	if err := bt.Run(); err != nil {
		return fmt.Errorf("run %s: %w", beatName, err)
	}
	return nil
}
