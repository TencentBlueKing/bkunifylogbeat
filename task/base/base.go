package base

import (
	"fmt"
	"github.com/TencentBlueKing/collector-go-sdk/v2/bkbeat/logp"
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

	End chan struct{}
}

func NewEmptyNode(id string) *Node {
	return &Node{
		ID:         id,
		ParentNode: nil,

		In:   make(chan interface{}),
		Outs: make(map[string]chan interface{}),

		End: make(chan struct{}),
	}
}

func (n *Node) String() string {
	if n.ParentNode != nil {
		return fmt.Sprintf("%s -> %s", n.ParentNode, n.ID)
	} else {
		return n.ID
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
		close(n.End)
	}
}

func (n *Node) Run() {
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
