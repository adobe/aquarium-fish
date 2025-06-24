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

// Author: Sergei Parshev (@sparshev)

// Package proxyssh implements SSH Proxy for user to get to the ApplicationResource
package proxyssh

import (
	"path/filepath"
	"sync"

	"golang.org/x/crypto/ssh"

	"github.com/adobe/aquarium-fish/lib/database"
	"github.com/adobe/aquarium-fish/lib/drivers/gate"
	"github.com/adobe/aquarium-fish/lib/log"
)

// Factory implements gate.DriverFactory interface
type Factory struct{}

// Name shows name of the gate factory
func (*Factory) Name() string {
	return "proxyssh"
}

// New creates new gate driver
func (f *Factory) New(db *database.Database) gate.Driver {
	return &Driver{
		db:   db,
		name: f.Name(),
	}
}

func init() {
	gate.FactoryList = append(gate.FactoryList, &Factory{})
}

// Driver implements drivers.ResourceDriver interface
type Driver struct {
	name string
	cfg  Config
	db   *database.Database

	// Proxy data
	serverConfig *ssh.ServerConfig

	// Keeps session info for auth, key is src address, value is session
	sessions sync.Map
}

// Name returns name of the gate
func (d *Driver) Name() string {
	return d.name
}

// Name returns name of the gate
func (d *Driver) SetName(name string) {
	d.name = name
}

// Prepare initializes the driver
func (d *Driver) Prepare(wd string, config []byte) (err error) {
	if err = d.cfg.Apply(config, d.db); err != nil {
		return err
	}
	if err = d.cfg.Validate(); err != nil {
		return err
	}

	keyPath := d.cfg.SSHKey
	if !filepath.IsAbs(keyPath) {
		keyPath = filepath.Join(wd, keyPath)
	}
	if d.cfg.BindAddress, err = d.proxyInit(keyPath); err != nil {
		return log.Errorf("PROXYSSH: %s: Unable to init proxyssh gate: %v", d.name, err)
	}

	return nil
}

// Shutdown gracefully stops the gate
func (*Driver) Shutdown() error {
	return nil
}

// GetRPCServices returns RPC services this gate driver wants to register
func (d *Driver) GetRPCServices() []gate.RPCService {
	return []gate.RPCService{
		d.newRPCHandler(),
	}
}
