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

//go:build jsonsonic

package json

import (
	stdjson "encoding/json"
	"reflect"
	"testing"

	"github.com/TencentBlueKing/bkmonitor-datalink/pkg/libgse/output/gse"
	"github.com/bytedance/sonic"
	"github.com/elastic/beats/libbeat/beat"
	"github.com/elastic/beats/libbeat/common"
)

func TestSonicImplementationEnabled(t *testing.T) {
	if sonic.APIKind != sonic.UseSonicJSON {
		t.Fatalf("sonic is using the encoding/json fallback: APIKind=%d", sonic.APIKind)
	}
}

func TestSonicMarshalCollectorEvent(t *testing.T) {
	fields := common.MapStr{
		"message": "hello <world> & 你好",
		"nested": common.MapStr{
			"enabled": true,
			"count":   int64(3),
		},
		"items": []interface{}{"one", int64(2), false},
	}

	encoderOutput, err := NewSonicEncoder().Encode("", &beat.Event{Fields: fields})
	if err != nil {
		t.Fatalf("encode collector event: %v", err)
	}
	assertJSONSemanticallyEqual(t, fields, encoderOutput)

	gseOutput, err := gse.MarshalFunc(fields)
	if err != nil {
		t.Fatalf("marshal GSE event: %v", err)
	}
	assertJSONSemanticallyEqual(t, fields, gseOutput)
}

func assertJSONSemanticallyEqual(t *testing.T, want interface{}, got []byte) {
	t.Helper()

	wantJSON, err := stdjson.Marshal(want)
	if err != nil {
		t.Fatalf("marshal expected JSON: %v", err)
	}

	var wantValue interface{}
	if err := stdjson.Unmarshal(wantJSON, &wantValue); err != nil {
		t.Fatalf("decode expected JSON: %v", err)
	}

	var gotValue interface{}
	if err := stdjson.Unmarshal(got, &gotValue); err != nil {
		t.Fatalf("decode sonic JSON: %v", err)
	}

	if !reflect.DeepEqual(wantValue, gotValue) {
		t.Fatalf("JSON value mismatch:\nwant: %s\n got: %s", wantJSON, got)
	}
}
