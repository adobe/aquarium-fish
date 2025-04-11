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
	"github.com/adobe/aquarium-fish/lib/db"
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
func (*Factory) New(d *db.Database) gate.Driver {
	return &Driver{db: d}
}

func init() {
	gate.FactoryList = append(gate.FactoryList, &Factory{})
}

// Driver implements drivers.ResourceDriver interface
type Driver struct {
	cfg Config
	db  *db.Database
}

// Name returns name of the gate
func (*Driver) Name() string {
	return "proxysocks"
}

// Prepare initializes the driver
func (d *Driver) Prepare(wd string, config []byte) error {
	if err := d.cfg.Apply(config); err != nil {
		return err
	}
	if err := d.cfg.Validate(); err != nil {
		return err
	}

	if err := proxyInit(d.db, d.cfg.BindAddress); err != nil {
		return log.Errorf("PROXYSOCKS: Unable to init proxysocks gate: %v", err)
	}

	return nil
}

// Shutdown gracefully stops the gate
func (*Driver) Shutdown() error {
	return nil
}
