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

package github

import (
	"encoding/json"
	"fmt"
	"path"
	"time"

	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/util"
)

const DefaultUpdateHooksInterval = time.Hour

// Config - node driver configuration
type Config struct {
	// Webhook Push config
	BindAddress string `json:"bind_address"` // Where to listen for incoming requests

	// Webhook Pull config
	APIToken string `json:"api_token"` // GitHub API personal fine-grained token

	APIAppID        int64  `json:"api_app_id"`         // GitHub App ID
	APIAppInstallID int64  `json:"api_app_install_id"` // GitHub App Installation ID
	APIAppKey       string `json:"api_app_key"`        // GitHub App private key in pem format

	APIPerPage int `json:"api_per_page"` // In case you want to save rate limit on lists, default: 100

	// Interval between hooks updates (set to -1 if don't want it to run periodically), default: 1h
	APIUpdateHooksInterval util.Duration `json:"api_update_hooks_interval"`
	// Interval between cleanups of runners (set to -1 if don't want to run it periodically), default: 1h
	APICleanupRunnersInterval util.Duration `json:"api_cleanup_runners_interval"`
	// Minimal interval in between deliveries checks, default: 30s
	APIMinCheckInterval util.Duration `json:"api_min_check_interval"`

	// Common configs
	// Filter contains pattern of the repos full name "org/repo" (accepts path.Match patterns)
	// and configuration. You have to set at least one filter ('*/*' for example)
	Filters map[string]Filter `json:"filters"`

	DeliveryValidInterval util.Duration `json:"delivery_valid_interval"`  // For how long to see the delivery as valid since it's delivery time, default: 30m
	DefaultJobMaxLifetime util.Duration `json:"default_job_max_lifetime"` // Used when job is stuck not completed and no lifetime is set for the label, default: 12h

	// If you need to use this gate with GitHub enterprise installation - set those configs
	EnterpriseBaseURL   string `json:"enterprise_base_url"`   // Format: http(s)://[hostname]/api/v3/
	EnterpriseUploadURL string `json:"enterprise_upload_url"` // Format: http(s)://[hostname]/api/uploads/

}

type Filter struct {
	// Acceptable secret of webhook requests, if not set - incoming requests will be skipped
	WebhookSecret string `json:"webhook_secret"`
}

// Apply takes json and applies it to the config structure
func (c *Config) Apply(config []byte) error {
	// Parse json
	if len(config) > 0 {
		if err := json.Unmarshal(config, c); err != nil {
			return log.Error("GITHUB: Unable to apply the gate config:", err)
		}
	}

	// Set default of pages per API request
	if c.APIPerPage == 0 {
		c.APIPerPage = 100
	}

	if c.APIUpdateHooksInterval == 0 {
		c.APIUpdateHooksInterval = util.Duration(time.Hour)
	}
	if c.APICleanupRunnersInterval == 0 {
		c.APICleanupRunnersInterval = util.Duration(time.Hour)
	}
	if c.APIMinCheckInterval <= 0 {
		c.APIMinCheckInterval = util.Duration(30 * time.Second)
	}

	if c.DeliveryValidInterval <= 0 {
		c.DeliveryValidInterval = util.Duration(30 * time.Minute)
	}
	if c.DefaultJobMaxLifetime <= 0 {
		c.DefaultJobMaxLifetime = util.Duration(12 * time.Hour)
	}

	return nil
}

// Validate makes sure the config have the required defaults & that the required fields are set
func (c *Config) Validate() (err error) {
	// In case none of the
	if !c.isWebhookEnabled() && !c.isAPIEnabled() {
		return fmt.Errorf("GITHUB: No configs specified to run the gate")
	}

	// Check patterns within Filters
	if len(c.Filters) == 0 {
		return fmt.Errorf("GITHUB: You need to set at least one pattern in filters")
	}
	for pattern := range c.Filters {
		if _, err := path.Match(pattern, ""); err != nil {
			return fmt.Errorf("GITHUB: Incorrect filter pattern: %v", err)
		}
	}

	return nil
}

// isWebhookEnabled returns true if bind address is set
func (c *Config) isWebhookEnabled() bool {
	return c.BindAddress != ""
}

// isAPIEnabled returns true if any of the auths are available
func (c *Config) isAPIEnabled() bool {
	return c.isAppAuth() || c.isTokenAuth()
}

// isAppAuth returns true if github app auth is available
func (c *Config) isAppAuth() bool {
	return c.APIAppID != 0 && c.APIAppInstallID != 0 && c.APIAppKey != ""
}

// isTokenAuth returns true if fine-grained token auth is available
func (c *Config) isTokenAuth() bool {
	return c.APIToken != ""
}
