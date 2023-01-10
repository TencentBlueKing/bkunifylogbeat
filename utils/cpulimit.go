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
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/TencentBlueKing/bkmonitor-datalink/pkg/libgse/logp"
	"github.com/shirou/gopsutil/process"
)

var (
	IsEnableRateLimiter bool      // 是否开启速率限制
	GlobalCpuLimiter    *CPULimit // CPU使用率限制
)

const (
	MaxCheckTimes = 10
)

type CPULimit struct {
	limit         int // cpu limit percent
	checkTimes    int // check cpu usage in one second
	checkInterval time.Duration

	isAllowRun bool

	closeOnce sync.Once
	done      chan struct{}
}

// NewCPULimit : new cpu limit object
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

// Start : start cpu limit
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
	timeToRunSeconds := timeToRun.Seconds()

	go func() {
		var (
			ticker       = time.NewTicker(c.GetCheckInterval())
			tickCount    = 0
			tickInterval = c.GetCheckInterval().Seconds()
		)
		defer ticker.Stop()

		lastCPUTime := time.Now()
		lastCPUTimes, err := c.GetCpuTimes(p)
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
				curCpuTimes, _ := c.GetCpuTimes(p)
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

// Stop : stop cpu limit
func (c *CPULimit) Stop() {
	c.closeOnce.Do(func() {
		close(c.done)
	})
	logp.L.Info("stop cpu limit.")
}

// Allow : judge current
func (c *CPULimit) Allow() bool {
	return c.isAllowRun
}

// GetCheckInterval : get cpu check interval
func (c *CPULimit) GetCheckInterval() time.Duration {
	return c.checkInterval
}

// SetResourceLimit 在一定程度上限制CPU使用
func SetResourceLimit(maxCpuLimit, checkTimes int) {
	numCPU := runtime.NumCPU()
	// 在docker富容器中 并且 开启了速率限制
	if IsInDocker() {
		logp.L.Infof("enable rate limit. because numOfCpu(%d) && isInDocker(%v)",
			numCPU, true,
		)

		if maxCpuLimit > 0 && maxCpuLimit <= numCPU*100 {
			GlobalCpuLimiter = NewCPULimit(maxCpuLimit, checkTimes)
			IsEnableRateLimiter = true
		} else {
			logp.L.Infof("disable rate limit. because cpu limit config(%d) is not valid",
				maxCpuLimit,
			)
		}
	} else {
		logp.L.Infof("disable rate limit. because numOfCpu(%d) && isInDocker(%v), "+
			"cpu limit config(%d)",
			numCPU, false, maxCpuLimit,
		)
		if GlobalCpuLimiter != nil {
			GlobalCpuLimiter.Stop()
		}
		IsEnableRateLimiter = false
	}
}
