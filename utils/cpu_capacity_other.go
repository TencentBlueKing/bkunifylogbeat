// Tencent is pleased to support the open source community by making bkunifylogbeat 蓝鲸日志采集器 available.
//
// Copyright (C) 2021 THL A29 Limited, a Tencent company. All rights reserved.
// Licensed under the MIT License.

//go:build !linux
// +build !linux

package utils

type fallbackCPUCapacityReader struct{}

func (fallbackCPUCapacityReader) Capacity() (cpuCapacity, error) {
	return cpuCapacity{Source: cpuCapacitySourceNonLinux}, nil
}

func newCPUCapacityReader() cpuCapacityReader {
	return fallbackCPUCapacityReader{}
}
