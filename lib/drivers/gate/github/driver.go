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

// Package github implements GitHub Actions gate to allow Webhooks to trigger Applications events
package github

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/google/go-github/v71/github"

	"github.com/adobe/aquarium-fish/lib/database"
	"github.com/adobe/aquarium-fish/lib/drivers/gate"
)

// Factory implements gate.DriverFactory interface
type Factory struct{}

// Name shows name of the gate factory
func (*Factory) Name() string {
	return "github"
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

	// Storing the github url here to use during workers provisioning
	githubURL string

	// Keeping the rate available to properly distribute it's resource over time
	apiRate      github.Rate
	apiRateMutex sync.Mutex

	// API checkpoint is needed to not process myriads of deliveries if we already processed them
	apiCheckpoint time.Time

	// Cache of the available hooks to quickly check for deliveries
	hooks      []*github.Hook
	hooksMutex sync.RWMutex

	// Needed for GitHub App auth to share TCP connections
	tr http.RoundTripper

	// Client requests need to be serial, without it it's relatively easy to hit secondary limits
	cl      *github.Client
	clMutex sync.Mutex

	// This mutex is needed to prevent simultaneous processing of received webhook & API delivery
	webhooksMutex sync.Mutex

	// Keeping track of running routines to gracefully shutdown the gate
	running       context.Context //nolint:containedctx
	runningCancel context.CancelFunc
	routines      sync.WaitGroup
	routinesMutex sync.Mutex
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
func (d *Driver) Prepare(wd string, config []byte) error {
	if err := d.cfg.Apply(config); err != nil {
		return err
	}
	if err := d.cfg.Validate(); err != nil {
		return err
	}

	if d.cfg.EnterpriseBaseURL == "" {
		d.githubURL = "https://github.com"
	} else {
		parsedURL, err := url.Parse(d.cfg.EnterpriseBaseURL)
		if err != nil {
			return fmt.Errorf("Unable to parse EnterpriseBaseURL: %v", err)
		}
		baseURL := &url.URL{
			Scheme: parsedURL.Scheme,
			Host:   parsedURL.Host,
		}
		d.githubURL = baseURL.String()
	}

	// Set checkpoint to reasonable time, othwerise it's doomed to process all the deliveries
	d.apiCheckpoint = time.Now().Add(-time.Duration(d.cfg.DeliveryValidInterval))

	// Init common shared transport
	d.tr = http.DefaultTransport

	d.running, d.runningCancel = context.WithCancel(context.Background())
	return d.init()
}

// Shutdown gracefully stops the gate
func (d *Driver) Shutdown() error {
	d.runningCancel()
	d.routines.Wait()
	return nil
}
