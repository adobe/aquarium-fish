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

// Package proxysocks allows host VM's to reach the remote services in controllable manner
package proxysocks

import (
	"github.com/adobe/aquarium-fish/lib/database"
	"github.com/adobe/aquarium-fish/lib/drivers/gate"
	"github.com/adobe/aquarium-fish/lib/log"
)

// Factory implements gate.DriverFactory interface
type Factory struct{}

// Name shows name of the gate factory
func (*Factory) Name() string {
	return "proxysocks"
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
}

// Name returns name of the gate
func (d *Driver) Name() string {
	return d.name
}

// SetName allows to receive the actual name of the driver
func (d *Driver) SetName(name string) {
	d.name = name
}

// Prepare initializes the driver
func (d *Driver) Prepare( /*wd*/ _ string, config []byte) error {
	if err := d.cfg.Apply(config); err != nil {
		return err
	}
	if err := d.cfg.Validate(); err != nil {
		return err
	}

	if err := d.proxyInit(); err != nil {
		return log.Errorf("PROXYSOCKS: %s: Unable to init proxysocks gate: %v", d.name, err)
	}

	return nil
}

// Shutdown gracefully stops the gate
func (*Driver) Shutdown() error {
	return nil
}
