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

//go:build linux
// +build linux

package utils

import (
	"bytes"
	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/process"
	"github.com/tklauser/go-sysconf"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

var ClockTicks = 100 // default value

func init() {
	clkTck, err := sysconf.Sysconf(sysconf.SC_CLK_TCK)
	// ignore errors
	if err == nil {
		ClockTicks = int(clkTck)
	}
}

// GetCpuTimes 获取指定进程的CPU时间统计信息。
// p: 指向 process.Process 的指针，代表要查询的进程。
// 返回值:
//
//	*cpu.TimesStat: 指向 cpu.TimesStat 的指针，包含CPU时间统计信息。
//	error: 如果发生错误，将返回相应的错误信息。
func (c *CPULimit) GetCpuTimes(p *process.Process) (*cpu.TimesStat, error) {
	return GetCpuTime()
}

// GetEnv retrieves the environment variable key. If it does not exist it returns the default.
// github.com/shirou/gopsutil@v3.21.8+incompatible/internal/common/common.go
func GetEnv(key string, dfault string, combineWith ...string) string {
	value := os.Getenv(key)
	if value == "" {
		value = dfault
	}

	switch len(combineWith) {
	case 0:
		return value
	case 1:
		return filepath.Join(value, combineWith[0])
	default:
		all := make([]string, len(combineWith)+1)
		all[0] = value
		copy(all[1:], combineWith)
		return filepath.Join(all...)
	}
}

// copy from github.com/shirou/gopsutil@v3.21.8+incompatible/process/process_linux.go
func splitProcStat(content []byte) []string {
	nameStart := bytes.IndexByte(content, '(')
	nameEnd := bytes.LastIndexByte(content, ')')
	restFields := strings.Fields(string(content[nameEnd+2:])) // +2 skip ') '
	name := content[nameStart+1 : nameEnd]
	pid := strings.TrimSpace(string(content[:nameStart]))
	fields := make([]string, 3, len(restFields)+3)
	fields[1] = string(pid)
	fields[2] = string(name)
	fields = append(fields, restFields...)
	return fields
}

// GetCpuTime copy from github.com/shirou/gopsutil@v3.21.8+incompatible/process/process_linux.go
func GetCpuTime() (*cpu.TimesStat, error) {
	pid := os.Getpid()
	var statPath = GetEnv("HOST_PROC", "/proc", strconv.Itoa(pid), "stat")

	contents, err := ioutil.ReadFile(statPath)
	if err != nil {
		return nil, err
	}
	// Indexing from one, as described in `man proc` about the file /proc/[pid]/stat
	fields := splitProcStat(contents)
	utime, err := strconv.ParseFloat(fields[14], 64)
	if err != nil {
		return nil, err
	}

	stime, err := strconv.ParseFloat(fields[15], 64)
	if err != nil {
		return nil, err
	}

	// There is no such thing as iotime in stat file.  As an approximation, we
	// will use delayacct_blkio_ticks (aggregated block I/O delays, as per Linux
	// docs).  Note: I am assuming at least Linux 2.6.18
	var iotime float64
	if len(fields) > 42 {
		iotime, err = strconv.ParseFloat(fields[42], 64)
		if err != nil {
			iotime = 0 // Ancient linux version, most likely
		}
	} else {
		iotime = 0 // e.g. SmartOS containers
	}

	return &cpu.TimesStat{
		CPU:    "cpu",
		User:   utime / float64(ClockTicks),
		System: stime / float64(ClockTicks),
		Iowait: iotime / float64(ClockTicks),
	}, nil
}
