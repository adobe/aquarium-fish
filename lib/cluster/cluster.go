/**
 * Copyright 2023 Adobe. All rights reserved.
 * This file is licensed to you under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License. You may obtain a copy
 * of the License at http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software distributed under
 * the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
 * OF ANY KIND, either express or implied. See the License for the specific language
 * governing permissions and limitations under the License.
 */

package cluster

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"

	"github.com/adobe/aquarium-fish/lib/fish"
	"github.com/adobe/aquarium-fish/lib/log"
)

type ClusterInfo struct {
	UID       uuid.UUID
	UpdatedAt time.Time
}

type Cluster struct {
	fish *fish.Fish

	// Contains info about the cluster to be permanently stored
	info ClusterInfo

	// Where the info will be stored
	cluster_file string

	clients []*Client

	ca_pool *x509.CertPool
	certkey tls.Certificate

	// Fired when cluster is ready and completed the sync process
	Ready chan bool
}

func New(fish *fish.Fish, join []string, data_dir, ca_path, cert_path, key_path string) (*Cluster, error) {
	c := &Cluster{
		fish:    fish,
		ca_pool: x509.NewCertPool(),
		Ready:   make(chan bool, 1),
	}

	// Load CA cert to pool
	ca_bytes, err := os.ReadFile(ca_path)
	if err != nil {
		return nil, fmt.Errorf("Cluster: Unable to load CA certificate: %v", err)
	}
	if !c.ca_pool.AppendCertsFromPEM(ca_bytes) {
		return nil, fmt.Errorf("Cluster: Incorrect CA pem data: %s", ca_path)
	}

	// Load client cert and key
	c.certkey, err = tls.LoadX509KeyPair(cert_path, key_path)
	if err != nil {
		return nil, fmt.Errorf("Cluster: Unable to load cert/key: %v", err)
	}

	// Read the cluster info if it's existing
	c.cluster_file = filepath.Join(data_dir, "cluster.yml")
	data, err := os.ReadFile(c.cluster_file)
	if err == nil {
		if err := yaml.Unmarshal(data, c); err != nil {
			return nil, fmt.Errorf("Cluster: Unable to read cluster config file: %v", err)
		}
	}

	// Connect the join nodes
	if len(join) > 0 {
		log.Info("Cluster: Connecting to cluster:", join)
		for _, endpoint := range join {
			c.NewConnect(endpoint, "cluster/v1/connect")
		}

		// Wait until all the clients will be synced
		go c.waitForClientsSync()
	} else {
		// In case it's the first node in the cluster - then create it
		c.info.UID = uuid.New()
		c.info.UpdatedAt = time.Now()

		log.Info("Cluster: Creating new cluster UID:", c.info.UID)

		// Write the first cluster info file
		cl_data, err := yaml.Marshal(&c.info)
		if err != nil {
			return nil, fmt.Errorf("Cluster: Unable to prepare cluster state yaml: %v", err)
		}
		if err := os.WriteFile(c.cluster_file, cl_data, 0500); err != nil {
			return nil, fmt.Errorf("Cluster: Unable to write cluster state file: %v", err)
		}

		// New cluster is ready
		c.Ready <- true
	}

	return c, nil
}

func (c *Cluster) NewConnect(host, channel string) *Client {
	conn := NewClientInitiator(c.fish, c, url.URL{Scheme: "wss", Host: host, Path: channel})

	c.clients = append(c.clients, conn)

	return conn
}

func (c *Cluster) Stop() {
	for _, conn := range c.clients {
		conn.Stop()
	}
}

// Function waits until the active clients will be synchronized (sync operation completed)
func (c *Cluster) waitForClientsSync() {
	var all_synced bool

	for !all_synced {
		log.Debug("Cluster: Waiting for all conections get in sync...")
		time.Sleep(time.Second)

		all_synced = true
		for _, conn := range c.clients {
			// In case connection is failed - no need to wait for it
			if conn.ConnFail == nil && !conn.InSync {
				all_synced = false
				break
			}
		}
	}

	// Ok, seems all the clients now in sync
	c.Ready <- true
}

func (c *Cluster) GetInfo() ClusterInfo {
	return c.info
}
