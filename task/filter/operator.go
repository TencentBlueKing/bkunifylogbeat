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
	"fmt"
	"regexp"
	"strings"
)

func equal(a, b string) bool {
	return a == b
}

func notEqual(a, b string) bool {
	return a != b
}

func include(a, b string) bool {
	if strings.Contains(a, b) {
		return true
	} else {
		return false
	}
}

func exclude(a, b string) bool {
	if strings.Contains(a, b) {
		return false
	} else {
		return true
	}
}

func regexMatch(a, b string) bool {
	regexpObject, err := regexp.Compile(b)
	if err != nil {
		fmt.Println("正则表达式编译失败:", err)
		return false
	}

	if regexpObject.MatchString(a) {
		return true
	} else {
		return false
	}
}

func regexNotMatch(a, b string) bool {
	regexpObject, err := regexp.Compile(b)
	if err != nil {
		fmt.Println("正则表达式编译失败:", err)
		return false
	}

	if regexpObject.MatchString(a) {
		return false
	} else {
		return true
	}
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

func getOperation(op string) func(a, b string) bool {
	if op == "=" || op == "eq" {
		return equal
	} else if op == "!=" || op == "neq" {
		return notEqual
	} else if op == "include" {
		return include
	} else if op == "exclude" {
		return exclude
	} else if op == "regex" {
		return regexMatch
	} else if op == "nregex" {
		return regexNotMatch
	} else {
		return nil
	}
}
