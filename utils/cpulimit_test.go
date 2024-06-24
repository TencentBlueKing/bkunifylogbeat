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

package utils

import (
	"fmt"
	"github.com/shirou/gopsutil/process"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/TencentBlueKing/bkmonitor-datalink/pkg/libgse/logp"
	libbeatlogp "github.com/elastic/beats/libbeat/logp"
)

// busyCpu
func busyCpu(limit *CPULimit, done chan struct{}) {
	for {
		select {
		case <-done:
			return
		default:
			//
		}

		if limit.Allow() {
			// do nothing
		} else {
			time.Sleep(limit.GetCheckInterval())
		}
	}
}
func testCPULimit(t *testing.T, taskNum int, coreNum int) {
	logp.SetLogger(libbeatlogp.L())
	var (
		limit        = 50           // CPU使用率限制
		checkTimes   = 10           // CPU检测频率（1秒内）
		toleranceVal = 10 * coreNum // 容忍值，允许在限制范围内，正负一定的容忍值
		done         = make(chan struct{}, 1)
	)

	// 设置GOMAXPROCS
	runtime.GOMAXPROCS(coreNum)

	p, err := process.NewProcess(int32(os.Getpid()))
	if err != nil {
		fmt.Printf("cpu limit, get process error=>(%v)", err)
		return
	}

	l := NewCPULimit(limit, checkTimes)
	defer l.Stop()

	_, _ = p.Percent(0)

	// Run tasks
	for i := 0; i < taskNum; i++ {
		go busyCpu(l, done)
	}

	count, testTimes := 0, 10
	tick := time.NewTicker(time.Second)
	defer tick.Stop()

L:
	for {
		select {
		case <-tick.C:
			usage, _ := p.Percent(0)
			fmt.Printf("current cpu usage is (%.2f%%)\n", usage)
			if usage > float64(limit+toleranceVal) {
				t.Errorf("cpu limit check false, usage(%.2f), expect limit(%d)", usage, limit)
			}

			count++
			if count >= testTimes {
				break L
			}
		}
	}
	close(done)
}

func TestNewCPULimit_SingleCore_SingleTask(t *testing.T) {
	testCPULimit(t, 1, 1)
}

func TestNewCPULimit_MultiCore_SingleTask(t *testing.T) {
	testCPULimit(t, 1, runtime.NumCPU())
}

func TestNewCPULimit_SingleCore_MultiTask(t *testing.T) {
	testCPULimit(t, 10, 1)
}

func TestNewCPULimit_MultiCore_MultiTask(t *testing.T) {
	testCPULimit(t, 10, runtime.NumCPU())
}
