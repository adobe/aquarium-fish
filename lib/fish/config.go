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
	"io/ioutil"
	"os"
	"time"

	"github.com/adobe/aquarium-fish/lib/util"
	"github.com/ghodss/yaml"
)

type Config struct {
	Directory string `json:"directory"` // Where to store database and other useful data (if relative - to CWD)

	APIAddress   string   `json:"api_address"`   // Where to serve Web UI, API & Meta API
	ProxyAddress string   `json:"proxy_address"` // Where to serve SOCKS5 proxy for the allocated resources
	NodeAddress  string   `json:"node_address"`  // What is the external address of the node
	ClusterJoin  []string `json:"cluster_join"`  // The node addresses to join the cluster

	TLSKey   string `json:"tls_key"`    // TLS PEM private key (if relative - to directory)
	TLSCrt   string `json:"tls_crt"`    // TLS PEM public certificate (if relative - to directory)
	TLSCaCrt string `json:"tls_ca_crt"` // TLS PEM certificate authority certificate (if relative - to directory)

	NodeName     string `json:"node_name"`     // Last resort in case you need to override the default host node name
	NodeLocation string `json:"node_location"` // Specify cluster node location for multi-dc configurations

	DefaultResourceLifetime string `json:"default_resource_lifetime"` // Sets the lifetime of the resource which will be used if label definition one is not set

	Drivers []ConfigDriver `json:"drivers"` // If specified - only the listed plugins will be loaded
}

type ConfigDriver struct {
	Name string            `json:"name"`
	Cfg  util.UnparsedJson `json:"cfg"`
}

func (c *Config) ReadConfigFile(cfg_path string) error {
	c.initDefaults()

	if cfg_path != "" {
		// Open and parse
		data, err := ioutil.ReadFile(cfg_path)
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
	c.Directory = "fish_data"
	c.APIAddress = "0.0.0.0:8001"
	c.ProxyAddress = "0.0.0.0:1080"
	c.NodeAddress = "127.0.0.1:8001"
	c.TLSKey = "" // Will be set after read config file from NodeName
	c.TLSCrt = "" // ...
	c.TLSCaCrt = "ca.crt"
	c.NodeName, _ = os.Hostname()
}
