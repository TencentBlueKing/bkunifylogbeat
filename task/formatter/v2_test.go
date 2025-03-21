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

package formatter

import (
	"testing"
	"time"

	beats "github.com/TencentBlueKing/bkmonitor-datalink/pkg/libgse/beat"
	"github.com/elastic/beats/filebeat/input/file"
	"github.com/elastic/beats/filebeat/util"
	"github.com/elastic/beats/libbeat/beat"
	"github.com/elastic/beats/libbeat/common"
	"github.com/stretchr/testify/assert"

	"github.com/TencentBlueKing/bkunifylogbeat/config"
)

func TestV2Formatter(t *testing.T) {
	vars := map[string]interface{}{
		"dataid":             "999990001",
		"harvester_limit":    10,
		"remove_path_prefix": "/data/bcs/docker/var/lib/docker/containers/f3616d188d0462018dc281373995c69765c2d91c39af60ee37501d65f28054ce",
		"is_container_std":   true,
	}
	taskConfig, err := config.CreateTaskConfig(vars)
	if err != nil {
		panic(err)
	}
	f, err := NewV2Formatter(taskConfig)
	if err != nil {
		panic(err)
	}

	event := &util.Data{
		Event: beat.Event{
			Timestamp: time.Now(),
			Fields: common.MapStr{
				"data":     "Hello from the Kubernetes cluster",
				"stream":   "stdout",
				"log_time": "2021-11-16T07:44:40.609753191Z",
			},
		},
	}
	event.SetState(file.State{Source: "/data/bcs/docker/var/lib/docker/containers/f3616d188d0462018dc281373995c69765c2d91c39af60ee37501d65f28054ce/f3616d188d0462018dc281373995c69765c2d91c39af60ee37501d65f28054ce-json.log"})

	data := f.Format([]*util.Data{event})

	assert.Equal(t, data["items"].([]beats.MapStr)[0]["log_time"], event.Event.Fields["log_time"])

	assert.Equal(t, data["items"].([]beats.MapStr)[0]["data"], event.Event.Fields["data"])
}

func TestV2Formatter_Multi(t *testing.T) {
	vars := map[string]interface{}{
		"dataid":             "999990001",
		"harvester_limit":    10,
		"remove_path_prefix": "/data/bcs/docker/var/lib/docker/containers/f3616d188d0462018dc281373995c69765c2d91c39af60ee37501d65f28054ce",
		"is_container_std":   true,
	}
	taskConfig, err := config.CreateTaskConfig(vars)
	if err != nil {
		panic(err)
	}
	f, err := NewV2Formatter(taskConfig)
	if err != nil {
		panic(err)
	}

	event := &util.Data{
		Event: beat.Event{
			Timestamp: time.Now(),
			Texts: []string{
				"Hello from the Kubernetes cluster",
				"Goodbye from the Kubernetes cluster",
			},
		},
	}
	event.SetState(file.State{Source: "/data/bcs/docker/var/lib/docker/containers/f3616d188d0462018dc281373995c69765c2d91c39af60ee37501d65f28054ce/f3616d188d0462018dc281373995c69765c2d91c39af60ee37501d65f28054ce-json.log"})

	data := f.Format([]*util.Data{event})

	assert.Equal(t, data["items"].([]LineItem)[0].Data, event.Event.Texts[0])

	assert.Equal(t, data["items"].([]LineItem)[1].Data, event.Event.Texts[1])
}

func TestV2FormatterMountReplace(t *testing.T) {
	vars := map[string]interface{}{
		"dataid":          "999990001",
		"harvester_limit": 10,
		"mounts": []config.Mount{
			{"/var/lib/kubelet/pods/ab7e4dcd-4c93-4f01-8ebd-fb7bcc293bd9/volumes/kubernetes.io~csi/pvc-38265a73-baa1-4249-879b-af4bbc30a7ba/mount/sub", "/data/datahub/backup"},
			{"/var/lib/kubelet/pods/ab7e4dcd-4c93-4f01-8ebd-fb7bcc293bd9/volumes/kubernetes.io~csi/pvc-38265a73-baa1-4249-879b-af4bbc30a7ba/mount", "/data/datahub/backup/deeper"},
			{"/data/bcs/service/docker/overlay2/1780a6fb393c8462d50edd775f0f7c89bf0769c2ede03259e190a8ad2007043c/merged", ""},
			{"/test/sub/mount", "/mount"},
		},
		"remove_path_prefix": "/var/host",
		"is_container_std":   true,
	}
	taskConfig, err := config.CreateTaskConfig(vars)
	if err != nil {
		panic(err)
	}
	f, err := NewV2Formatter(taskConfig)
	if err != nil {
		panic(err)
	}

	event := &util.Data{
		Event: beat.Event{
			Timestamp: time.Now(),
			Fields: common.MapStr{
				"data":     "Hello from the Kubernetes cluster",
				"stream":   "stdout",
				"log_time": "2021-11-16T07:44:40.609753191Z",
			},
		},
	}
	event.SetState(file.State{Source: "/var/host/data/bcs/service/docker/overlay2/1780a6fb393c8462d50edd775f0f7c89bf0769c2ede03259e190a8ad2007043c/merged/data/datahub/udp/backup/a/b/c.log"})

	data := f.Format([]*util.Data{event})

	assert.Equal(t, data["filename"], "/data/datahub/udp/backup/a/b/c.log")

	event.SetState(file.State{Source: "/var/host/var/lib/kubelet/pods/ab7e4dcd-4c93-4f01-8ebd-fb7bcc293bd9/volumes/kubernetes.io~csi/pvc-38265a73-baa1-4249-879b-af4bbc30a7ba/mount/d/e/f.log"})

	data = f.Format([]*util.Data{event})

	assert.Equal(t, data["filename"], "/data/datahub/backup/deeper/d/e/f.log")

}
