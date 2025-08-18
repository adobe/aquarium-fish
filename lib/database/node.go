/**
 * Copyright 2021-2025 Adobe. All rights reserved.
 * This file is licensed to you under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License. You may obtain a copy
 * of the License at http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software distributed under
 * the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
 * OF ANY KIND, either express or implied. See the License for the specific language
 * governing permissions and limitations under the License.
 */

// Author: Sergei Parshev (@sparshev)

package database

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/google/uuid"

	typesv2 "github.com/adobe/aquarium-fish/lib/types/aquarium/v2"
)

func (d *Database) subscribeNodeImpl(_ context.Context, ch chan NodeSubscriptionEvent) {
	subscribeHelper(d, &d.subsNode, ch)
}

// unsubscribeNodeImpl removes a channel from the subscription list
func (d *Database) unsubscribeNodeImpl(_ context.Context, ch chan NodeSubscriptionEvent) {
	unsubscribeHelper(d, &d.subsNode, ch)
}

// nodeListImpl returns list of Nodes that fits filter
func (d *Database) nodeListImpl(_ context.Context) (ns []typesv2.Node, err error) {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	err = d.be.Collection(ObjectNode).List(&ns)
	return ns, err
}

// nodeGetImpl returns Node by it's unique name
func (d *Database) nodeGetImpl(_ context.Context, name string) (node *typesv2.Node, err error) {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	err = d.be.Collection(ObjectNode).Get(name, &node)
	return node, err
}

// nodeActiveListImpl lists all the nodes in the cluster
func (d *Database) nodeActiveListImpl(ctx context.Context) (ns []typesv2.Node, err error) {
	// Only the nodes that pinged at least twice the delay time
	t := time.Now().Add(-typesv2.NodePingDelay * 2 * time.Second)
	all, err := d.NodeList(ctx)
	if err != nil {
		return ns, err
	}
	for _, n := range all {
		if t.Before(n.UpdatedAt) {
			ns = append(ns, n)
		}
	}
	return ns, err
}

// nodeCreateImpl makes new Node
func (d *Database) nodeCreateImpl(_ context.Context, n *typesv2.Node) error {
	if n.Name == "" {
		return fmt.Errorf("node name can't be empty")
	}
	if n.Pubkey == nil {
		return fmt.Errorf("node should be initialized before create")
	}

	d.beMu.RLock()
	defer d.beMu.RUnlock()

	// Create node UUID based on the public key
	hash := sha256.New()
	hash.Write(n.Pubkey)
	n.Uid = uuid.NewHash(hash, uuid.UUID{}, n.Pubkey, 0)
	n.CreatedAt = time.Now()
	n.UpdatedAt = n.CreatedAt
	err := d.be.Collection(ObjectNode).Add(n.Name, n)

	if err == nil {
		// Notify subscribers about the new Node
		notifySubscribersHelper(d, &d.subsNode, NewCreateEvent(n), ObjectNode)
	}

	return err
}

// nodeSaveImpl stores Node
func (d *Database) nodeSaveImpl(_ context.Context, node *typesv2.Node) error {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	node.UpdatedAt = time.Now()
	err := d.be.Collection(ObjectNode).Add(node.Name, node)

	if err == nil {
		// Notify subscribers about the removed Node
		notifySubscribersHelper(d, &d.subsNode, NewUpdateEvent(node), ObjectNode)
	}

	return err
}

// nodePingImpl updates Node and shows that it's active
func (d *Database) nodePingImpl(ctx context.Context) error {
	d.nodeMu.Lock()
	defer d.nodeMu.Unlock()
	return d.NodeSave(ctx, d.node)
}

// SetNode puts current node in the memory storage
func (d *Database) SetNode(node *typesv2.Node) {
	d.nodeMu.Lock()
	defer d.nodeMu.Unlock()
	d.node = node
}

// GetNode returns current Fish node spec
func (d *Database) GetNode() typesv2.Node {
	d.nodeMu.RLock()
	defer d.nodeMu.RUnlock()
	return *d.node
}

// GetNodeUID returns node UID
func (d *Database) GetNodeUID() typesv2.NodeUID {
	d.nodeMu.RLock()
	defer d.nodeMu.RUnlock()
	return d.node.Uid
}

// GetNodeName returns current node name
func (d *Database) GetNodeName() string {
	d.nodeMu.RLock()
	defer d.nodeMu.RUnlock()
	return d.node.Name
}

// GetNodeAddress returns node external address
func (d *Database) GetNodeAddress() string {
	d.nodeMu.RLock()
	defer d.nodeMu.RUnlock()
	return d.node.Address
}

// GetNodeLocation returns current node location
func (d *Database) GetNodeLocation() string {
	d.nodeMu.RLock()
	defer d.nodeMu.RUnlock()
	return d.node.Location
}
