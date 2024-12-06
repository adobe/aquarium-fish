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
	"fmt"
	"os"
	"time"

	"github.com/adobe/aquarium-fish/lib/util"
	"github.com/ghodss/yaml"
)

// Config defines Fish node configuration
type Config struct {
	Directory string `json:"directory"` // Where to store database and other useful data (if relative - to CWD)

	APIAddress        string         `json:"api_address"`         // Where to serve Web UI, API & Meta API
	ProxySocksAddress string         `json:"proxy_socks_address"` // Where to serve SOCKS5 proxy for the allocated resources
	ProxySSHAddress   string         `json:"proxy_ssh_address"`   // Where to serve SSH proxy for the allocated resources
	NodeAddress       string         `json:"node_address"`        // What is the external address of the node
	CPULimit          uint16         `json:"cpu_limit"`           // How many CPU threads Node allowed to use (serve API, ...)
	MemTarget         util.HumanSize `json:"mem_target"`          // What's the target memory utilization by the Node (GC target where it becomes more aggressive)
	ClusterJoin       []string       `json:"cluster_join"`        // The node addresses to join the cluster

	TLSKey   string `json:"tls_key"`    // TLS PEM private key (if relative - to directory)
	TLSCrt   string `json:"tls_crt"`    // TLS PEM public certificate (if relative - to directory)
	TLSCaCrt string `json:"tls_ca_crt"` // TLS PEM certificate authority certificate (if relative - to directory)

	NodeName        string   `json:"node_name"`        // Last resort in case you need to override the default host node name
	NodeLocation    string   `json:"node_location"`    // Specify cluster node location for multi-dc configurations
	NodeIdentifiers []string `json:"node_identifiers"` // The list of node identifiers which could be used to find the right Node for Resource

	NodeSSHKey string `json:"ssh_key"` // The SSH RSA identity private key for the fish node (if relative - to directory)

	DefaultResourceLifetime string `json:"default_resource_lifetime"` // Sets the lifetime of the resource which will be used if label definition one is not set

	// Configuration for the node drivers, if defined - only the listed plugins will be loaded
	// Each configuration could instantinate the same driver multiple times by adding instance name
	// separated from driver by slash symbol (like "<driver>/prod" - will create "prod" instance).
	Drivers []ConfigDriver `json:"drivers"`
}

// ConfigDriver helper to store driver config without parsing it right away
type ConfigDriver struct {
	Name string            `json:"name"`
	Cfg  util.UnparsedJSON `json:"cfg"`
}

// ReadConfigFile needed to read the config file
func (c *Config) ReadConfigFile(cfgPath string) error {
	c.initDefaults()

	if cfgPath != "" {
		// Open and parse
		data, err := os.ReadFile(cfgPath)
		if err != nil {
			return err
		}

		if err := yaml.Unmarshal(data, c); err != nil {
			return err
		}
	}

	if c.TLSKey == "" {
		c.TLSKey = c.NodeName + ".key"
	}
	if c.TLSCrt == "" {
		c.TLSCrt = c.NodeName + ".crt"
	}

	if c.NodeSSHKey == "" {
		c.NodeSSHKey = c.NodeName + "_id_ecdsa"
	}

	_, err := time.ParseDuration(c.DefaultResourceLifetime)
	if c.DefaultResourceLifetime != "" && err != nil {
		return fmt.Errorf("Fish: Default Resource Lifetime parse error: %v", err)
	}

	return nil
}

func (c *Config) initDefaults() {
	c.Directory = "fish_data"
	c.APIAddress = "0.0.0.0:8001"
	c.ProxySocksAddress = "0.0.0.0:1080"
	c.ProxySSHAddress = "0.0.0.0:2022"
	c.NodeAddress = "127.0.0.1:8001"
	c.TLSKey = "" // Will be set after read config file from NodeName
	c.TLSCrt = "" // ...
	c.TLSCaCrt = "ca.crt"
	c.NodeName, _ = os.Hostname()
}
