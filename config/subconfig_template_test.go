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

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileSubConfigTemplatesForwardFieldExtraction(t *testing.T) {
	templatePaths, err := filepath.Glob("../support-files/templates/*/*/etc/bkunifylogbeat.conf.tpl")
	if err != nil {
		t.Fatalf("find file subconfig templates: %v", err)
	}
	if len(templatePaths) != 6 {
		t.Fatalf("expected 6 file subconfig templates, got %d", len(templatePaths))
	}

	requiredFragments := []string{
		"{% if item.field_extraction is defined %}",
		"field_extraction:",
		"pattern: '{{ item.field_extraction.get('pattern', '') | replace(\"'\", \"''\") }}'",
		"{% if item.field_extraction.deduplication is defined %}",
		"deduplication:",
		"enabled: {{ item.field_extraction.deduplication.get('enabled', '') | lower }}",
		"window: '{{ item.field_extraction.deduplication.window }}'",
		"max_keys: {{ item.field_extraction.deduplication.max_keys | int }}",
		"max_total_keys: {{ item.field_extraction.deduplication.max_total_keys | int }}",
	}

	for _, templatePath := range templatePaths {
		t.Run(filepath.ToSlash(templatePath), func(t *testing.T) {
			content, err := os.ReadFile(templatePath)
			if err != nil {
				t.Fatalf("read template: %v", err)
			}
			for _, fragment := range requiredFragments {
				if !strings.Contains(string(content), fragment) {
					t.Errorf("template does not forward %q", fragment)
				}
			}
		})
	}
}
