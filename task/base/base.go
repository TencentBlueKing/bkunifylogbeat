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

package base

import (
	"fmt"
	"sync"

	"github.com/TencentBlueKing/bkmonitor-datalink/pkg/libgse/logp"
	bkmonitoring "github.com/TencentBlueKing/bkmonitor-datalink/pkg/libgse/monitoring"
	"github.com/elastic/beats/libbeat/monitoring"
)

var (
	CrawlerSendTotal        = bkmonitoring.NewInt("crawler_send_total")         // 兼容指标，之前有使用这个作为发送的数量
	CrawlerReceived         = bkmonitoring.NewInt("crawler_received")           // 兼容指标，接收到的所有事件数
	CrawlerState            = bkmonitoring.NewInt("crawler_state")              // 兼容指标，接收到的所有事件中状态事件数量
	CrawlerDropped          = bkmonitoring.NewInt("crawler_dropped")            // 兼容指标，丢弃数量
	CrawlerPackageSendTotal = bkmonitoring.NewInt("crawler_package_send_total") // 打包发送的数量
)

type NodeI interface {
	SetInput(input chan interface{})
	AddOutput(node *Node)
	RemoveOutput(node *Node)
	Run()
}

type Node struct {
	ID         string
	ParentNode *Node

	Outs map[string]chan interface{}
	In   chan interface{}

	CloseOnce sync.Once
	End       chan struct{}

	GameOver chan struct{} // 用该信号代表Run函数已经完整退出

	TaskNodeList map[string]map[string]*TaskNode
}

type TaskNode struct {
	*Node

	CrawlerReceived  *monitoring.Int //state事件
	CrawlerState     *monitoring.Int //state事件
	CrawlerSendTotal *monitoring.Int //正常事件总数
	CrawlerDropped   *monitoring.Int //过滤掉的事件总数

	SenderReceive   *monitoring.Int // 接收的事件数
	SenderSendTotal *monitoring.Int // 发送到pipeline的数量
	SenderState     *monitoring.Int // 仅需要更新采集状态的事件数(event.Field为空)
}

func NewEmptyNode(id string) *Node {
	return &Node{
		ID:         id,
		ParentNode: nil,

		In:   make(chan interface{}),
		Outs: make(map[string]chan interface{}),

		End: make(chan struct{}),

		GameOver: make(chan struct{}),

		TaskNodeList: map[string]map[string]*TaskNode{},
	}
}

func (n *Node) String() string {
	if n.ParentNode != nil {
		return fmt.Sprintf("%s -> %s", n.ParentNode, n.ID)
	} else {
		return n.ID
	}
}

func (n *Node) AddTaskNode(nextNode *Node, taskNode *TaskNode) {
	if nextNode == nil || taskNode == nil {
		return
	}
	nextNodeToTaskNodeList, ok := n.TaskNodeList[nextNode.ID]
	if !ok {
		nextNodeToTaskNodeList = map[string]*TaskNode{
			taskNode.ID: taskNode,
		}
		n.TaskNodeList[nextNode.ID] = nextNodeToTaskNodeList
	} else {
		nextNodeToTaskNodeList[taskNode.ID] = taskNode
	}
}

func (n *Node) RemoveTaskNode(nextNode *Node, taskNode *TaskNode) {
	if nextNode == nil || taskNode == nil {
		return
	}

	if n.ParentNode != nil {
		n.ParentNode.RemoveTaskNode(n, taskNode)
	}
	delete(n.TaskNodeList[nextNode.ID], taskNode.ID)
	if len(n.TaskNodeList[nextNode.ID]) == 0 {
		delete(n.TaskNodeList, nextNode.ID)
	}
}

func (n *Node) SetInput(input chan interface{}) {
	if input == nil {
		logp.L.Error("should not add nil input!")
		return
	}
	n.In = input
}

func (n *Node) AddOutput(node *Node) {
	if node == nil {
		logp.L.Error("should not add nil output!")
		return
	}
	// 记录父节点，为了释放的时候，可以从后往前遍历Node
	node.ParentNode = n
	n.Outs[node.ID] = node.In
}

func (n *Node) RemoveOutput(node *Node) {
	if node == nil {
		logp.L.Error("should not remove nil output!")
		return
	}
	// 一层层往上释放
	delete(n.Outs, node.ID)
	if len(n.Outs) == 0 {
		if n.ParentNode != nil {
			n.ParentNode.RemoveOutput(n)
		}
		logp.L.Infof("node(%s) is remove", n.ID)
		n.CloseOnce.Do(func() {
			close(n.End)
			n.WaitUntilGameOver()
		})
	}
}

func (n *Node) Run() {
	defer close(n.GameOver)
	for {
		select {
		case <-n.End:
			// node is done
			return
		case e := <-n.In:
			// do anything by yourself
			event := e
			//event := n.handle(e)
			for _, out := range n.Outs {
				out <- event
			}
		}
	}
}

func (n *Node) WaitUntilGameOver() {
	select {
	case <-n.GameOver:
		return
	}
}
