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

	"gopkg.in/yaml.v3"

	"github.com/adobe/aquarium-fish/lib/util"
)

type Config struct {
	Directory string `yaml:"directory"` // Where to store database and other useful data (if relative - to CWD)

	APIAddress   string `yaml:"api_address"`   // Where to serve Web UI, API & Meta API
	ProxyAddress string `yaml:"proxy_address"` // Where to serve SOCKS5 proxy for the allocated resources
	NodeAddress  string `yaml:"node_address"`  // What is the external address of the node

	ClusterJoin []string `yaml:"cluster_join"` // The node addresses to join the cluster
	ClusterAuto bool     `yaml:"cluster_auto"` // Automatic cluster management (if you need to have only the configured connections)

	TLSKey   string `yaml:"tls_key"`    // TLS PEM private key (if relative - to directory)
	TLSCrt   string `yaml:"tls_crt"`    // TLS PEM public certificate (if relative - to directory)
	TLSCaCrt string `yaml:"tls_ca_crt"` // TLS PEM certificate authority certificate (if relative - to directory)

	NodeName        string   `yaml:"node_name"`        // Last resort in case you need to override the default host node name
	NodeLocation    string   `yaml:"node_location"`    // Specify cluster node location for multi-dc configurations
	NodeIdentifiers []string `yaml:"node_identifiers"` // The list of node identifiers which could be used to find the right Node for Resource

	DefaultResourceLifetime string `yaml:"default_resource_lifetime"` // Sets the lifetime of the resource which will be used if label definition one is not set

	Drivers []ConfigDriver `yaml:"drivers"` // If specified - only the listed plugins will be loaded
}

type ConfigDriver struct {
	Name string            `yaml:"name"`
	Cfg  util.UnparsedJson `yaml:"cfg"`
}

func (c *Config) ReadConfigFile(cfg_path string) error {
	c.initDefaults()

	if cfg_path != "" {
		// Open and parse
		data, err := os.ReadFile(cfg_path)
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

	_, err := time.ParseDuration(c.DefaultResourceLifetime)
	if c.DefaultResourceLifetime != "" && err != nil {
		return fmt.Errorf("Fish: Default Resource Lifetime parse error: %v", err)
	}

	return nil
}

func (c *Config) initDefaults() {
	c.ClusterAuto = true
	c.Directory = "fish_data"
	c.APIAddress = "0.0.0.0:8001"
	c.ProxyAddress = "0.0.0.0:1080"
	c.NodeAddress = "" // Will be replaced by the API bind to the proper address with port
	c.TLSKey = ""      // Will be set after read config file from NodeName
	c.TLSCrt = ""      // ...
	c.TLSCaCrt = "ca.crt"
	c.NodeName, _ = os.Hostname()
}
