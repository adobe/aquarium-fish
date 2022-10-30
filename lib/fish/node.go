/**
 * Copyright 2021 Adobe. All rights reserved.
 * This file is licensed to you under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License. You may obtain a copy
 * of the License at http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software distributed under
 * the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
 * OF ANY KIND, either express or implied. See the License for the specific language
 * governing permissions and limitations under the License.
 */

package fish

import (
	"crypto/sha256"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"

	"github.com/adobe/aquarium-fish/lib/openapi/types"
)

func (f *Fish) NodeFind(filter *string) (ns []types.Node, err error) {
	db := f.db
	if filter != nil {
		db = db.Where(*filter)
	}
	err = db.Find(&ns).Error
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
		return fmt.Errorf("Fish: Name can't be empty")
	}
	if n.Pubkey == nil {
		return fmt.Errorf("Fish: Node should be initialized before create")
	}

	// Create node UUID based on the public key
	hash := sha256.New()
	hash.Write(*n.Pubkey)
	n.UID = uuid.NewHash(hash, uuid.UUID{}, *n.Pubkey, 0)
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
