/**
 * Copyright 2025 Adobe. All rights reserved.
 * This file is licensed to you under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License. You may obtain a copy
 * of the License at http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software distributed under
 * the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
 * OF ANY KIND, either express or implied. See the License for the specific language
 * governing permissions and limitations under the License.
 */

// Database management for the Fish node
package db

import (
	"os"
	"path/filepath"

	"github.com/google/uuid"
	"go.mills.io/bitcask/v2"

	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/openapi/types"
)

var ErrObjectNotFound = bitcask.ErrObjectNotFound

// Database implements necessary functions to manipulate the internal database
type Database struct {
	// Backend used to store the data
	be *bitcask.Bitcask

	// Memory storage for current node - we oftenly using it to generate new UIDs
	node types.Node
}

// Init creates the database object by provided path
func New(path string) (*Database, error) {
	if err := os.MkdirAll(path, 0o750); err != nil {
		return nil, log.Errorf("DB: Can't create working directory %s: %v", path, err)
	}

	be, err := bitcask.Open(filepath.Join(path, "bitcask.db"))
	if err != nil {
		return nil, log.Errorf("DB: Unable to initialize database: %v", err)
	}

	return &Database{be: be}, nil
}

// CompactDB runs stale Applications and data removing
func (d *Database) CompactDB() error {
	log.Debug("DB: CompactDB running...")
	defer log.Debug("Fish: CompactDB done")

	s, _ := d.be.Stats()
	log.Debugf("DB: CompactDB: Before compaction: Datafiles: %d, Keys: %d, Size: %d, Reclaimable: %d", s.Datafiles, s.Keys, s.Size, s.Reclaimable)

	if err := d.be.Merge(); err != nil {
		return log.Errorf("DB: CompactDB: Merge operation failed: %v", err)
	}

	s, _ = d.be.Stats()
	log.Debugf("DB: CompactDB: After compaction: Datafiles: %d, Keys: %d, Size: %d, Reclaimable: %d", s.Datafiles, s.Keys, s.Size, s.Reclaimable)

	return nil
}

// Shutdown compacts database backend and closes it
func (d *Database) Shutdown() error {
	d.CompactDB()

	if err := d.be.Close(); err != nil {
		return log.Errorf("DB: Unable to compact backend: %v", err)
	}

	return nil
}

// SetNode puts current node in the memory storage
func (d *Database) SetNode(node types.Node) {
	d.node = node
}

// GetNode returns current Fish node spec
func (d *Database) GetNode() *types.Node {
	return &d.node
}

// GetNodeUID returns node UID
func (d *Database) GetNodeUID() types.ApplicationUID {
	return d.node.UID
}

// GetNodeName returns current node name
func (d *Database) GetNodeName() string {
	return d.node.Name
}

// GetNodeLocation returns current node location
func (d *Database) GetNodeLocation() string {
	return d.node.Location
}

// NewUID Creates new UID with 6 starting bytes of Node UID as prefix
func (d *Database) NewUID() uuid.UUID {
	uid := uuid.New()
	copy(uid[:], d.node.UID[:6])
	return uid
}
