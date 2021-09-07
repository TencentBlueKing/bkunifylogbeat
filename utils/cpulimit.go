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
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/TencentBlueKing/collector-go-sdk/v2/bkbeat/logp"
	"github.com/shirou/gopsutil/process"
)

type CPULimit struct {
	limit         int // cpu limit percent
	checkTimes    int // check cpu usage in one second
	checkInterval time.Duration

	isAllowRun bool

	closeOnce sync.Once
	done      chan struct{}
}

const (
	MaxCheckTimes = 10
)

// NewCPULimit: new cpu limit object
func NewCPULimit(l, checkTimes int) *CPULimit {
	if checkTimes < 1 || checkTimes > MaxCheckTimes {
		checkTimes = MaxCheckTimes
	}

	cpuLimit := &CPULimit{
		limit:         l,
		checkTimes:    checkTimes,
		checkInterval: time.Duration(float64(time.Second) / float64(checkTimes)),

		isAllowRun: true,
		done:       make(chan struct{}, 1),
	}
	cpuLimit.Start()
	return cpuLimit
}

// Start: start cpu limit
func (c *CPULimit) Start() {
	logp.L.Info("start cpu limit.")

	p, err := process.NewProcess(int32(os.Getpid()))
	if err != nil {
		logp.L.Errorf("cpu limit, get process error=>(%v)", err)
		return
	}

	numCPU := runtime.NumCPU()
	if c.limit < 0 || c.limit > numCPU*100 {
		logp.L.Errorf("cpu limit, limit(%d) config not valid, cpu core(%d)",
			c.limit, numCPU)
		return
	}

	// 在1s内可运行的时长
	timeToRun := time.Duration((float64(time.Second)) * (float64(c.limit) / 100))
	logp.L.Infof("cpu limit, max time to run in second is %s \n", timeToRun)
	fmt.Printf("cpu limit, max time to run in second is %s \n", timeToRun)
	timeToRunSeconds := timeToRun.Seconds()

	go func() {
		var (
			ticker       = time.NewTicker(c.GetCheckInterval())
			tickCount    = 0
			tickInterval = c.GetCheckInterval().Seconds()
		)
		defer ticker.Stop()

		lastCPUTime := time.Now()
		lastCPUTimes, err := p.Times()
		if err != nil {
			logp.L.Errorf("cpu limit, get cpu stat err=>(%+v)", err)
			return
		}
		for {
			select {
			case <-c.done:
				c.isAllowRun = true
				return
			case <-ticker.C:
				now := time.Now()
				curCpuTimes, _ := p.Times()
				delta := now.Sub(lastCPUTime).Seconds()
				deltaCPUTime := curCpuTimes.Total() - lastCPUTimes.Total()
				if deltaCPUTime > timeToRunSeconds-tickInterval {
					c.isAllowRun = false
				} else {
					c.isAllowRun = true
				}

				tickCount++
				if tickCount == c.checkTimes {
					tickCount = 0
					c.isAllowRun = true
					lastCPUTimes = curCpuTimes
					lastCPUTime = now
				}

				logp.L.Debugf("%d current cpu usage is =>(%.2f%%), "+
					"cpu use time is =>(%.2f), "+
					"time elapsed is =>(%.2f), "+
					"time allow run in seconds =>(%.2f), "+
					"isAllowRun(%v)\n",
					tickCount, deltaCPUTime/delta*100, deltaCPUTime,
					delta, timeToRunSeconds, c.isAllowRun)
			}
		}
	}()
}

// Stop: stop cpu limit
func (c *CPULimit) Stop() {
	c.closeOnce.Do(func() {
		close(c.done)
	})
	logp.L.Info("stop cpu limit.")
}

// Allow: judge current
func (c *CPULimit) Allow() bool {
	return c.isAllowRun
}

// GetCheckInterval: get cpu check interval
func (c *CPULimit) GetCheckInterval() time.Duration {
	return c.checkInterval
}
