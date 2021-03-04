package fish

import (
	"errors"
	"time"
)

const NODE_PING_DELAY = 30

var NodePingDuplicationErr = errors.New("Fish Node: Unable to join the Aquarium cluster due to " +
	"the node with the same name are pinged the cluster less then 2xNODE_PING_DELAY time ago")

type Node struct {
	ID        uint `gorm:"primaryKey"`
	CreatedAt time.Time
	UpdatedAt time.Time // Updates every 30s, protects connecting node with the same name in 60s interval
	// Unable to use SoftDelete due to error during Save https://gorm.io/docs/delete.html#Soft-Delete

	Name       string         `gorm:"unique"` // Unique name of the node
	Definition NodeDefinition // Verbose information about the node
}

func (e *App) NodeCreate(node *Node) error {
	return e.db.Create(node).Error
}

func (e *App) NodeSave(node *Node) error {
	return e.db.Save(node).Error
}

func (e *App) NodePing(node *Node) error {
	return e.db.Model(node).Update("name", node.Name).Error
}

func (e *App) NodeGet(name string) (node *Node, err error) {
	node = &Node{}
	err = e.db.Where("name = ?", name).First(node).Error
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

func (e *App) ping() error {
	// In order to optimize network & database - update just UpdatedAt field
	ping_ticker := time.NewTicker(NODE_PING_DELAY * time.Second)
	for {
		select {
		case <-ping_ticker.C:
			e.NodePing(e.node)
		}
	}
	return nil
}
