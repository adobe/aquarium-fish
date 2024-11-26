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
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/openapi/types"
	"github.com/adobe/aquarium-fish/lib/util"
)

// NodeFind returns list of Nodes that fits filter
func (f *Fish) NodeFind(filter *string) (ns []types.Node, err error) {
	db := f.db
	if filter != nil {
		securedFilter, err := util.ExpressionSQLFilter(*filter)
		if err != nil {
			log.Warn("Fish: SECURITY: weird SQL filter received:", err)
			// We do not fail here because we should not give attacker more information
			return ns, nil
		}
		db = db.Where(securedFilter)
	}
	err = db.Find(&ns).Error
	return ns, err
}

// NodeGet returns Node by it's unique name
func (f *Fish) NodeGet(name string) (node *types.Node, err error) {
	node = &types.Node{}
	err = f.db.Where("name = ?", name).First(node).Error
	return node, err
}

// NodeActiveList lists all the nodes in the cluster
func (f *Fish) NodeActiveList() (ns []types.Node, err error) {
	// Only the nodes that pinged at least twice the delay time
	t := time.Now().Add(-types.NodePingDelay * 2 * time.Second)
	err = f.db.Where("updated_at > ?", t).Find(&ns).Error
	return ns, err
}

// NodeCreate makes new Node
func (f *Fish) NodeCreate(n *types.Node) error {
	if err := n.Validate(); err != nil {
		return fmt.Errorf("Fish: Unable to validate Node: %v", err)
	}

	// Create node UUID based on the public key
	hash := sha256.New()
	hash.Write(*n.Pubkey)
	n.UID = uuid.NewHash(hash, uuid.UUID{}, *n.Pubkey, 0)
	return f.db.Create(n).Error
}

// NodeSave stores Node
func (f *Fish) NodeSave(node *types.Node) error {
	return f.db.Save(node).Error
}

// NodePing updates Node and shows that it's active
func (f *Fish) NodePing(node *types.Node) error {
	return f.db.Model(node).Update("name", node.Name).Error
}

func (f *Fish) NodeGetPubkey(pubkey []byte) (nodes []types.Node, err error) {
	err = f.db.Where("pubkey = ?", pubkey).Find(&nodes).Error
	return nodes, err
}

func (f *Fish) pingProcess() {
	// TODO: Clean up this ping process and switch to cluster websocket one
	// In order to optimize network & database - update just UpdatedAt field
	pingTicker := time.NewTicker(types.NodePingDelay * time.Second)
	for {
		if !f.running {
			break
		}

		// TODO: Here should be select with quit in case app is stopped to not wait next ticker
		<-pingTicker.C
		log.Debug("Fish Node: ping")
		f.NodePing(f.node)
	}
}

// Insert / update the node directly from the data, without changing created_at and updated_at
func (f *Fish) NodeImport(n *types.Node) error {
	if err := n.Validate(); err != nil {
		return fmt.Errorf("Fish: Unable to validate Node: %v", err)
	}

	// The updated_at and created_at should stay the same so skipping the hooks
	tx := f.db.Session(&gorm.Session{SkipHooks: true})
	err := tx.Create(n).Error
	if err != nil {
		err = tx.Save(n).Error
	}

	return err
}
