package fish

import (
	"errors"
	"log"
	"time"
)

const NODE_PING_DELAY = 30

var NodePingDuplicationErr = errors.New("Fish Node: Unable to join the Aquarium cluster due to " +
	"the node with the same name are pinged the cluster less then 2xNODE_PING_DELAY time ago")

type Node struct {
	ID        int64 `gorm:"primaryKey"`
	CreatedAt time.Time
	UpdatedAt time.Time // Updates every 30s, protects connecting node with the same name in 60s interval
	// Unable to use SoftDelete due to error during Save https://gorm.io/docs/delete.html#Soft-Delete

	Name       string         `json:"name" gorm:"unique"` // Unique name of the node
	Definition NodeDefinition `json:"definition"`         // Verbose information about the node
}

func (f *Fish) NodeList() (ns []Node, err error) {
	err = f.db.Find(&ns).Error
	return ns, err
}

func (f *Fish) NodeCreate(n *Node) error {
	if n.Name == "" {
		return errors.New("Fish: Name can't be empty")
	}

	return f.db.Create(n).Error
}

func (f *Fish) NodeSave(node *Node) error {
	return f.db.Save(node).Error
}

func (f *Fish) NodePing(node *Node) error {
	return f.db.Model(node).Update("name", node.Name).Error
}

func (f *Fish) NodeGet(name string) (node *Node, err error) {
	node = &Node{}
	err = f.db.Where("name = ?", name).First(node).Error
	return node, err
}

func (n *Node) Init() error {
	curr_time := time.Now()

	// Allow to join cluster only if it's the new node or the existing node
	// was here 2xNODE_PING_DELAY seconds ago to prevent duplication
	if curr_time.Before(n.UpdatedAt.Add(NODE_PING_DELAY * 2 * time.Second)) {
		return NodePingDuplicationErr
	}

	// Collect the node definition data
	n.Definition.Update()

	return nil
}

func (f *Fish) pingProcess() error {
	// In order to optimize network & database - update just UpdatedAt field
	ping_ticker := time.NewTicker(NODE_PING_DELAY * time.Second)
	for {
		select {
		case <-ping_ticker.C:
			log.Println("Fish Node: ping")
			f.NodePing(f.node)
		}
	}
	return nil
}
