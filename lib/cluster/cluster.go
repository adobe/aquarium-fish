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

	"github.com/adobe/aquarium-fish/lib/cluster/msg"
	"github.com/adobe/aquarium-fish/lib/fish"
	"github.com/adobe/aquarium-fish/lib/log"
)

type ClusterInfo struct {
	// Which cluster data is used in the internal database
	UID uuid.UUID

	// This field used as "last sync point" for the incoming cluster data
	UpdatedAt time.Time
}

type Cluster struct {
	fish *fish.Fish
	hub  *Hub

	// Contains info about the cluster to be permanently stored
	info ClusterInfo

	// The map contains types allowed for sync, because not all the types needed to be synced
	allowed_sync_types map[string]func(*msg.Message)

	// Where the info will be stored
	cluster_file string

	clients []*Client

	ca_pool *x509.CertPool
	certkey tls.Certificate

	// Fired when cluster is ready and completed the sync process
	InSync bool
	Ready  chan bool

	// Optimization to skip the processed messages and not broadcast again during cluster operation
	// They are not stay here for long - just for ~2 minutes while cluster quickly syncs the data
	processed_sums *sumCache
}

func New(fish *fish.Fish, join []string, data_dir, ca_path, cert_path, key_path string) (*Cluster, error) {
	c := &Cluster{
		fish:    fish,
		hub:     newHub(),
		ca_pool: x509.NewCertPool(),
		Ready:   make(chan bool, 1),

		processed_sums: newSumCache(time.Minute*2, time.Second*30),
	}

	// Fill the list of allowed to sync types
	c.allowed_sync_types = map[string]func(*msg.Message){
		"User":             c.importUser,
		"Label":            c.importLabel,
		"Application":      c.importApplication,
		"ApplicationState": c.importApplicationState,
		"ApplicationTask":  c.importApplicationTask,
		"ServiceMapping":   c.importServiceMapping,
		"Vote":             c.importVote,
		"Location":         c.importLocation,
		"Node":             c.importNode,
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
	if err := c.readClusterInfo(); err != nil {
		return nil, fmt.Errorf("Cluster: Unable to read cluster config file: %v", err)
	}

	// Connecting to the cluster or creating it
	if len(join) > 0 {
		// When we have join list - try those addresses to sync first
		// Useful on initial cluster join and if the cluster nodes were changed since last sync
		log.Info("Cluster: Connecting to existing cluster:", join)
		for _, endpoint := range join {
			c.NewConnect(endpoint, "cluster/v1/connect")
		}

		// Wait until all the clients will be synced
		go c.waitForSync()
	} else if c.info.UID != uuid.Nil {
		// When cluster UID is here - use the available nodes info to connect and sync with them
		log.Info("Cluster: Connecting to known cluster:", join)
		// TODO: Cluster is existing, but we need to run clients and sync them from previous good state
		c.Ready <- true // Just for now
		//go c.waitForSync()
		go c.watchConnectionsProcess()
	} else {
		// In case it's the first node in the cluster - then create it
		c.info.UID = uuid.New()
		c.info.UpdatedAt = time.Now()

		// Write the first cluster info file
		if err := c.writeClusterInfo(); err != nil {
			return nil, fmt.Errorf("Cluster: Unable to create cluster info file: %v", err)
		}

		log.Info("Cluster: Created new cluster UID:", c.info.UID)

		// New cluster is ready
		c.Ready <- true

		// Cluster is ready, run the background watcher
		go c.watchConnectionsProcess()
	}

	return c, nil
}

// Writes the cluster info file which is used to sync after node restart
// Usually running by timer during the regular cluster stamp update process
func (c *Cluster) writeClusterInfo() error {
	cl_data, err := yaml.Marshal(&c.info)
	if err != nil {
		return fmt.Errorf("Cluster: Unable to prepare cluster state yaml: %v", err)
	}

	if _, err := os.Stat(c.cluster_file); !os.IsNotExist(err) {
		// Make sure we have backup of the cluster data - in case of fail it will stay and allow
		// for easy restore of the node in exchange for a bit longer sync process
		os.Remove(c.cluster_file + ".bak")
		if err := os.Rename(c.cluster_file, c.cluster_file+".bak"); err != nil {
			return fmt.Errorf("Cluster: Unable to rename the cluster info file %q: %v", c.cluster_file, err)
		}
	}

	// Writing the new file right in place of the actual cluster file since it was moved before
	if err := os.WriteFile(c.cluster_file, cl_data, 0600); err != nil {
		return fmt.Errorf("Cluster: Unable to write cluster state file: %v", err)
	}
	return nil
}

// Reads the current cluster info from the yaml file, in case there is an error in reading the file
// the backup will be used instead
func (c *Cluster) readClusterInfo() (out error) {
	if _, err := os.Stat(c.cluster_file); !os.IsNotExist(err) {
		if data, err := os.ReadFile(c.cluster_file); err == nil {
			if err = yaml.Unmarshal(data, &c.info); err == nil {
				return nil
			}
			out = log.Error("Cluster: Unable to parse cluster config file:", c.cluster_file, err)
		} else {
			out = log.Error("Cluster: Unable to read cluster config file:", c.cluster_file, err)
		}
	}

	// Try to read the backup file since previous try failed
	bak_config := c.cluster_file + ".bak"
	if _, err := os.Stat(bak_config); !os.IsNotExist(err) {
		if data, err := os.ReadFile(bak_config); err == nil {
			if err = yaml.Unmarshal(data, &c.info); err == nil {
				return nil
			}
			out = log.Error("Cluster: Unable to parse cluster config file backup:", bak_config, err)
		} else {
			out = log.Error("Cluster: Unable to read cluster config file backup:", bak_config, err)
		}
	}

	return out
}

// Checks the filled map of the type-importing and used for sending too
func (c *Cluster) ImportTypeAllowed(type_name string) (func(*msg.Message), bool) {
	f, ok := c.allowed_sync_types[type_name]
	return f, ok
}

func (c *Cluster) NewConnect(host, channel string) *Client {
	conn := NewClientInitiator(c.fish, c, url.URL{Scheme: "wss", Host: host, Path: channel})

	c.clients = append(c.clients, conn)

	return conn
}

func (c *Cluster) GetHub() *Hub {
	return c.hub
}

func (c *Cluster) Send(message *msg.Message) error {
	if ok := c.processed_sums.Put(message.Sum); !ok {
		// The message was already processed by the cluster so skipping
		return nil
	}
	log.Debug("Cluster: Broadcasting message:", message.Type)
	if err := c.hub.Broadcast(message); err != nil {
		c.processed_sums.Delete(message.Sum)
		return log.Error("Cluster: Unable to broadcast message:", err)
	}
	return nil
}

func (c *Cluster) Stop() {
	for _, conn := range c.clients {
		conn.Stop()
	}
}

// Function waits until the active clients will be synchronized (sync operation completed)
func (c *Cluster) waitForSync() {
	var in_sync bool
	for {
		// When we've got the clients - need to trigger sync and wait for it
		for _, conn := range c.clients {
			// Triggering the client sync if it's connected but not in sync with the cluster.
			// It's running sequentially over each connected client to put not much pressure on
			// the cluster and simplifies the sync parallelization.
			if conn.IsConnected() {
				conn.SyncRequest()
				// Waiting for sync to complete
				for {
					log.Info("Cluster: Waiting for cluster sync from client:", conn.ident)
					// In case connection is failed - no need to wait for it
					if conn.ConnFail != nil || !conn.IsConnected() {
						log.Warn("Cluster: Failed to wait for sync from client:", conn.ident)
						break
					}
					if !conn.IsLongOperationExecuting("sync") {
						// Marking cluster as  in sync
						in_sync = true
						break
					}
					time.Sleep(time.Second)
				}
			}
		}

		if in_sync {
			break
		}

		log.Info("Cluster: Waiting for any conection to sync the cluster...")
		time.Sleep(time.Second)
	}

	// Ok, seems all the clients now in sync
	log.Info("Cluster: Sync is done, cluster is ready:", c.info.UpdatedAt)
	c.InSync = true
	c.Ready <- true

	// Cluster is ready, run the background watcher
	go c.watchConnectionsProcess()
}

func (c *Cluster) GetInfo() ClusterInfo {
	return c.info
}

// Background watcher to estblish enough connections to the other cluster nodes
func (c *Cluster) watchConnectionsProcess() {
	// Running the background process to write cluster info file periodically
	go c.infoUpdateProcess()

	// TODO: Run watch on the nodes and ensure there is ~8 connections available (configurable),
	// ensure most of the connections (~90%) are to the same location and rest to the other ones
}

// This function needed to periodically write the cluster info file, otherwise it will be written
// way too often and will make disk worn-out quicker then we would love to
func (c *Cluster) infoUpdateProcess() {
	prev_time := c.info.UpdatedAt
	update_ticker := time.NewTicker(time.Minute)
	for {
		if !c.fish.IsRunning() {
			break
		}
		select {
		case <-update_ticker.C:
			// Write only when the cluster was updated since previous time
			if prev_time.Before(c.info.UpdatedAt) {
				log.Debug("Cluster: update info")
				if err := c.writeClusterInfo(); err != nil {
					log.Error("Cluster: Unable to write cluster info:", err)
				} else {
					prev_time = c.info.UpdatedAt
				}
			}
		}
	}
}
