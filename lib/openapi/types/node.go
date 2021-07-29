package types

import (
	"errors"
	"time"
)

const NODE_PING_DELAY = 30

var NodePingDuplicationErr = errors.New("Fish Node: Unable to join the Aquarium cluster due to " +
	"the node with the same name pinged the cluster less then 2xNODE_PING_DELAY time ago")

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
