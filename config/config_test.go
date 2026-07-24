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
	"testing"

	"github.com/elastic/beats/libbeat/common"
	"github.com/stretchr/testify/assert"
)

func TestLoadConfig(t *testing.T) {
	content := `
tasks:
- dataid: 1573203
  docker-json:
    cri_flags: true
    force_cri_logs: false
    partial: true
    stream: all
  ext_meta:
    container_id: f855feda5406cc8d55ee5dd3b736626e0b0f12caa2b2f7ade13fe60645e930e2
    container_image: sha256:8522d622299ca431311ac69992419c956fbaca6fa8289c76810c9399d17c69de
    container_name: install-cni
    io_kubernetes_pod: kube-flannel-ds-pzlt6
    io_kubernetes_pod_namespace: kube-system
    io_kubernetes_pod_uid: d6d61a0c-92af-46cb-ad99-f7213d227654
    io_kubernetes_workload_name: kube-flannel-ds
    io_kubernetes_workload_type: DaemonSet
  input: tail
  paths:
    - /var/host/data/bcs/lib/docker/containers/930e2-json.log
  remove_path_prefix: /var/host
  tail_files: true
- dataid: 1573203
  docker-json:
    cri_flags: true
    force_cri_logs: false
    partial: true
    stream: all
  ext_meta:
    container_id: f855feda5406cc8d55ee5dd3b736626e0b0f12caa2b2f7ade13fe60645e930e2
    container_image: sha256:8522d622299ca431311ac69992419c956fbaca6fa8289c76810c9399d17c69de
    container_name: install-cni
    io_kubernetes_pod: kube-flannel-ds-pzlt7
    io_kubernetes_pod_namespace: kube-system
    io_kubernetes_pod_uid: d6d61a0c-92af-46cb-ad99-f7213d227654
    io_kubernetes_workload_name: kube-flannel-ds
    io_kubernetes_workload_type: DaemonSet
  input: tail
  paths:
    - /var/host/data/bcs/lib/docker/containers/930e1-json.log
  remove_path_prefix: /var/host
  tail_files: true
`

	f, err := os.CreateTemp("", "tasks.conf")
	assert.NoError(t, err)
	f.WriteString(content)

	defer func() {
		f.Close()
		os.Remove(f.Name())
	}()

	c, err := common.LoadFile(f.Name())
	assert.NoError(t, err)

	var config Config
	assert.NoError(t, c.Unpack(&config))

	tasks := GetTasks(config)
	assert.Len(t, tasks, 2)

	for k, v := range tasks {
		t.Logf("task(%s): %#v", k, v)
	}
}
