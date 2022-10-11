package msg

type Nodes struct {
	Type string  `json:"type"`
	Data []*Node `json:"data"`
}

func NewNodes() *Nodes {
	return &Nodes{
		Type: "nodes",
	}
}

func (m *Nodes) AddNode(node *Node) {
	m.Data = append(m.Data, node)
}
