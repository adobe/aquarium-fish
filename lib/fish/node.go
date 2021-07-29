package fish

import (
	"errors"
	"log"
	"time"

	"github.com/adobe/aquarium-fish/lib/openapi/types"
)

func (f *Fish) NodeList() (ns []types.Node, err error) {
	err = f.db.Find(&ns).Error
	return ns, err
}

func (f *Fish) NodeActiveList() (ns []types.Node, err error) {
	// Only the nodes that pinged at least twice the delay time
	t := time.Now().Add(-types.NODE_PING_DELAY * 2 * time.Second)
	err = f.db.Where("updated_at > ?", t).Find(&ns).Error
	return ns, err
}

func (f *Fish) NodeCreate(n *types.Node) error {
	if n.Name == "" {
		return errors.New("Fish: Name can't be empty")
	}

	return f.db.Create(n).Error
}

func (f *Fish) NodeSave(node *types.Node) error {
	return f.db.Save(node).Error
}

func (f *Fish) NodePing(node *types.Node) error {
	return f.db.Model(node).Update("name", node.Name).Error
}

func (f *Fish) NodeGet(name string) (node *types.Node, err error) {
	node = &types.Node{}
	err = f.db.Where("name = ?", name).First(node).Error
	return node, err
}

func (f *Fish) pingProcess() error {
	// In order to optimize network & database - update just UpdatedAt field
	ping_ticker := time.NewTicker(types.NODE_PING_DELAY * time.Second)
	for {
		if !f.running {
			break
		}
		select {
		case <-ping_ticker.C:
			log.Println("Fish Node: ping")
			f.NodePing(f.node)
		}
	}
	return nil
}
