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
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/google/uuid"

	typesv2 "github.com/adobe/aquarium-fish/lib/types/aquarium/v2"
)

// NodeFind returns list of Nodes that fits filter
func (d *Database) NodeList() (ns []typesv2.Node, err error) {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	err = d.be.Collection(ObjectNode).List(&ns)
	return ns, err
}

// NodeGet returns Node by it's unique name
func (d *Database) NodeGet(name string) (node *typesv2.Node, err error) {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	err = d.be.Collection(ObjectNode).Get(name, &node)
	return node, err
}

// NodeActiveList lists all the nodes in the cluster
func (d *Database) NodeActiveList() (ns []typesv2.Node, err error) {
	// Only the nodes that pinged at least twice the delay time
	t := time.Now().Add(-typesv2.NodePingDelay * 2 * time.Second)
	all, err := d.NodeList()
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

// NodeCreate makes new Node
func (d *Database) NodeCreate(n *typesv2.Node) error {
	if n.Name == "" {
		return fmt.Errorf("Fish: Name can't be empty")
	}
	if n.Pubkey == nil {
		return fmt.Errorf("Fish: Node should be initialized before create")
	}

	d.beMu.RLock()
	defer d.beMu.RUnlock()

	// Create node UUID based on the public key
	hash := sha256.New()
	hash.Write(n.Pubkey)
	n.Uid = uuid.NewHash(hash, uuid.UUID{}, n.Pubkey, 0)
	n.CreatedAt = time.Now()
	n.UpdatedAt = n.CreatedAt
	return d.be.Collection(ObjectNode).Add(n.Name, n)
}

// NodeSave stores Node
func (d *Database) NodeSave(node *typesv2.Node) error {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	node.UpdatedAt = time.Now()
	return d.be.Collection(ObjectNode).Add(node.Name, node)
}

// NodePing updates Node and shows that it's active
func (d *Database) NodePing(node *typesv2.Node) error {
	return d.NodeSave(node)
}
