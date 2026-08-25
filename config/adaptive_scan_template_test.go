// Tencent is pleased to support the open source community by making bkunifylogbeat 蓝鲸日志采集器 available.
//
// Copyright (C) 2021 THL A29 Limited, a Tencent company. All rights reserved.
// Licensed under the MIT License.

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdaptiveScanDeploymentTemplates(t *testing.T) {
	platforms := []string{
		"aix/powerpc",
		"linux/aarch64",
		"linux/x86",
		"linux/x86_64",
		"windows/x86",
		"windows/x86_64",
	}
	projectVariable := `adaptive_scan_enabled:
              title: "AdaptiveScanEnabled(启用自适应文件扫描周期)"
              type: boolean
              default: false`
	templateConfig := `{%- if extra_vars is defined and extra_vars.adaptive_scan_enabled is defined %}
bkunifylogbeat.adaptive_scan.enabled: {{ extra_vars.adaptive_scan_enabled | lower }}
{%- else %}
bkunifylogbeat.adaptive_scan.enabled: false
{%- endif %}`

	for _, platform := range platforms {
		t.Run(strings.ReplaceAll(platform, "/", "_"), func(t *testing.T) {
			root := filepath.Join("..", "support-files", "templates", filepath.FromSlash(platform))
			assertFileContains(t, filepath.Join(root, "project.yaml"), projectVariable)
			assertFileContains(t, filepath.Join(root, "etc", "bkunifylogbeat_main.conf.tpl"), templateConfig)
		})
	}
}

func assertFileContains(t *testing.T, path, expected string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !strings.Contains(string(content), expected) {
		t.Errorf("%s does not contain expected adaptive scan configuration", path)
	}
}
