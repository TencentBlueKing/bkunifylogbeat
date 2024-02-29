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

package filter

import (
	"strings"
)

func equal(a, b string) bool {
	return a == b
}

func notEqual(a, b string) bool {
	return a != b
}

func include(text, subString string) bool {
	return strings.Contains(text, subString)
}

func exclude(text, subString string) bool {
	return !strings.Contains(text, subString)
}

// sequence same with config define
var operation = []func(a, b string) bool{
	equal,
	notEqual,
}

const (
	EqualOperation = iota
	NotEqualOperation
)

const (
	opEq       = "eq"
	opNeq      = "neq"
	opEqual    = "="
	opNotEqual = "!="
	opInclude  = "include"
	opExclude  = "exclude"
	opRegex    = "regex"
	opNregex   = "nregex"
)

func getOperation(op string) func(a, b string) bool {
	switch op {
	case opEqual, opEq:
		return equal
	case opNotEqual, opNeq:
		return notEqual
	case opInclude:
		return include
	case opExclude:
		return exclude
	default:
		return nil
	}
}
