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

const (
	max_clients            = 8 // Maximum amount of active connections for the cluster
	min_remote_loc_clients = 1 // Amount of connections need to be established with remote locations
)

var ErrAlreadyProcessedMessage = fmt.Errorf("The message is already processed, skipping")

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

	// Optimization to skip the processed messages and not broadcast again during cluster operation
	// They are not stay here for long - just for ~2 minutes while cluster quickly syncs the data
	processed_sums *sumCache

	// Option to enable or disable the clients automanagement
	autoManageClients bool
}

func New(fish *fish.Fish, join []string, data_dir, ca_path, cert_path, key_path string) (*Cluster, error) {
	cl := &Cluster{
		fish:    fish,
		hub:     newHub(),
		ca_pool: x509.NewCertPool(),

		processed_sums: newSumCache(time.Minute*2, time.Second*30),

		autoManageClients: fish.GetCfg().ClusterAuto,
	}

	// Fill the list of allowed to sync types
	cl.allowed_sync_types = map[string]func(*msg.Message){
		"User":             cl.importUser,
		"Label":            cl.importLabel,
		"Application":      cl.importApplication,
		"ApplicationState": cl.importApplicationState,
		"ApplicationTask":  cl.importApplicationTask,
		"ServiceMapping":   cl.importServiceMapping,
		"Vote":             cl.importVote,
		"Location":         cl.importLocation,
		"Node":             cl.importNode,
		"Resource":         cl.importResource,
	}

	// Load CA cert to pool
	ca_bytes, err := os.ReadFile(ca_path)
	if err != nil {
		return nil, fmt.Errorf("Cluster: Unable to load CA certificate: %v", err)
	}
	if !cl.ca_pool.AppendCertsFromPEM(ca_bytes) {
		return nil, fmt.Errorf("Cluster: Incorrect CA pem data: %s", ca_path)
	}

	// Load client cert and key
	cl.certkey, err = tls.LoadX509KeyPair(cert_path, key_path)
	if err != nil {
		return nil, fmt.Errorf("Cluster: Unable to load cert/key: %v", err)
	}

	// Read the cluster info if it's existing
	cl.cluster_file = filepath.Join(data_dir, "cluster.yml")
	if err := cl.readClusterInfo(); err != nil {
		return nil, fmt.Errorf("Cluster: Unable to read cluster config file: %v", err)
	}

	// Connecting to the cluster or creating it
	if len(join) > 0 {
		// When we have join list - try those addresses to sync the cluster
		// Useful on initial cluster join and if the cluster nodes were changed since last sync
		log.Info("Cluster: Connecting to existing cluster:", join)
		for _, endpoint := range join {
			cl.NewConnect(endpoint)
		}

		// Wait until the cluster will be synced
		if err := cl.waitForSync(); err != nil {
			return nil, fmt.Errorf("Cluster: Unable to sync with the join nodes: %v", err)
		}
	} else if cl.info.UID != uuid.Nil {
		// When cluster UID is here - use the available nodes info to connect and sync with them
		log.Info("Cluster: Connecting to known cluster:", join)
		// TODO: Cluster exists, but we need to run clients and sync them from previous good state
		//go cl.waitForSync()
	} else {
		// In case it's the first node in the cluster - then create it
		cl.info.UID = uuid.New()
		cl.info.UpdatedAt = time.Now()

		// Write the first cluster info file
		if err := cl.writeClusterInfo(); err != nil {
			return nil, fmt.Errorf("Cluster: Unable to create cluster info file: %v", err)
		}

		log.Info("Cluster: Created new cluster UID:", cl.info.UID)
	}

	// Ok, seems all the clients now in sync
	log.Info("Cluster: Sync is done, cluster is ready:", cl.info.UpdatedAt)
	cl.InSync = true

	// Cluster is ready, run the background watcher
	go cl.watchConnectionsProcess()

	return cl, nil
}

// Writes the cluster info file which is used to sync after node restart
// Usually running by timer during the regular cluster stamp update process
func (cl *Cluster) writeClusterInfo() error {
	cl_data, err := yaml.Marshal(&cl.info)
	if err != nil {
		return fmt.Errorf("Cluster: Unable to prepare cluster state yaml: %v", err)
	}

	if _, err := os.Stat(cl.cluster_file); !os.IsNotExist(err) {
		// Make sure we have backup of the cluster data - in case of fail it will stay and allow
		// for easy restore of the node in exchange for a bit longer sync process
		os.Remove(cl.cluster_file + ".bak")
		if err := os.Rename(cl.cluster_file, cl.cluster_file+".bak"); err != nil {
			return fmt.Errorf("Cluster: Unable to rename the cluster info file %q: %v", cl.cluster_file, err)
		}
	}

	// Writing the new file right in place of the actual cluster file since it was moved before
	if err := os.WriteFile(cl.cluster_file, cl_data, 0600); err != nil {
		return fmt.Errorf("Cluster: Unable to write cluster state file: %v", err)
	}
	return nil
}

// Reads the current cluster info from the yaml file, in case there is an error in reading the file
// the backup will be used instead
func (cl *Cluster) readClusterInfo() (out error) {
	if _, err := os.Stat(cl.cluster_file); !os.IsNotExist(err) {
		if data, err := os.ReadFile(cl.cluster_file); err == nil {
			if err = yaml.Unmarshal(data, &cl.info); err == nil {
				return nil
			}
			out = log.Error("Cluster: Unable to parse cluster config file:", cl.cluster_file, err)
		} else {
			out = log.Error("Cluster: Unable to read cluster config file:", cl.cluster_file, err)
		}
	}

	// Try to read the backup file since previous try failed
	bak_config := cl.cluster_file + ".bak"
	if _, err := os.Stat(bak_config); !os.IsNotExist(err) {
		if data, err := os.ReadFile(bak_config); err == nil {
			if err = yaml.Unmarshal(data, &cl.info); err == nil {
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
func (cl *Cluster) ImportTypeAllowed(type_name string) (func(*msg.Message), bool) {
	f, ok := cl.allowed_sync_types[type_name]
	return f, ok
}

func (cl *Cluster) NewConnect(address string) {
	// Sometimes address is yet unknown for the node, so skipping this node
	if address == "" {
		return
	}
	NewClientInitiator(cl.fish, cl, url.URL{Scheme: "wss", Host: address, Path: "cluster/v1/connect"})
}

func (cl *Cluster) GetHub() *Hub {
	return cl.hub
}

func (cl *Cluster) Send(message *msg.Message) error {
	if ok := cl.processed_sums.Put(message.Sum); !ok {
		// The message was already processed by the cluster so skipping
		return ErrAlreadyProcessedMessage
	}
	log.Debug("Cluster: Broadcasting message:", message.Type)
	if err := cl.hub.Broadcast(message); err != nil {
		cl.processed_sums.Delete(message.Sum)
		return log.Error("Cluster: Unable to broadcast message:", err)
	}
	return nil
}

func (cl *Cluster) Stop() {
	clients := cl.hub.Clients()
	for _, conn := range clients {
		conn.Stop()
	}
}

// Function waits until the active clients will be synchronized (sync operation completed)
func (cl *Cluster) waitForSync() error {
	var in_sync bool
	var conn_fails int
	for {
		clients := cl.hub.Clients()
		if len(clients) < 1 {
			// Wait for the clients to appear in the hub
			time.Sleep(20 * time.Millisecond)
			continue
		}

		conn_fails = 0
		// When we've got the clients - need to trigger sync and wait for it
		for _, conn := range clients {
			// Triggering the client sync if it's connected but not in sync with the cluster.
			// It's running sequentially over each connected client to put not much pressure on
			// the cluster and simplifies the sync parallelization.
			if conn.IsConnected() {
				conn.SyncRequest()
				// Waiting for sync to complete
				for {
					log.Info("Cluster: Waiting for cluster sync from client:", conn.Ident())
					// In case connection is failed - no need to wait for it
					if conn.ConnFail != nil || !conn.IsConnected() {
						log.Warn("Cluster: Failed to wait for sync from client:", conn.Ident())
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

			// Increasing the counter of failed connections
			if conn.ConnFail != nil {
				conn_fails += 1
			}
		}

		if in_sync {
			break
		}

		// Check if all the connections fail
		if conn_fails == len(clients) {
			return fmt.Errorf("Cluster: All the cluster clients failed to connect and unable to sync")
		}

		log.Info("Cluster: Waiting for any conection to sync the cluster...")
		time.Sleep(time.Second)
	}

	return nil
}

func (cl *Cluster) GetInfo() ClusterInfo {
	return cl.info
}

// Background watcher to estblish enough connections to the other cluster nodes
// Executed strictly after sync with the cluster is done
// TODO: It's extremely poorly written, need to find out the better way of doing the same
// TODO: Contains not yet ready logic to automanage the clients - will be done later
func (cl *Cluster) watchConnectionsProcess() {
	// Running the background process to write cluster info file periodically
	go cl.infoUpdateProcess()

	// Cache for checked nodes - we don't want to bother the other nodes of
	// the cluster too much, so keeping the items for 30 mins
	//checked_nodes := make(map[string]time.Time)

	// Check the available connections every 10 seconds
	conns_ticker := time.NewTicker(10 * time.Second)
	for {
		if !cl.fish.IsRunning() {
			break
		}
		select {
		case <-conns_ticker.C:
			if !cl.autoManageClients {
				log.Debug("Cluster: Client roster management disabled, skipping")
				continue
			}
			/*log.Debug("Cluster: Refreshing the clients roster")

			// Check if there are dead connections and there is a room for new ones
			clients := cl.hub.Clients()
			var bad_clients []*Client
			clients_uniq := make(map[string]*Client)
			for _, client := range clients {
				// Look for the duplicated connections, they've could be here because
				// the nodes could connect to each other simultaneously
				if _, exists := clients_uniq[client.Ident()]; exists {
					// It's a duplication so placing in bad clients and triggering disconnect
					bad_clients = append(bad_clients, client)
					continue
				}
				// Adding client to unique list since it's not a duplication
				clients_uniq[client.Ident()] = client

				// Obviously if the client is failed - it's a candidate to replacement
				if client.ConnFail != nil {
					bad_clients = append(bad_clients, client)
					continue
				}
			}

			// Clean up the bad clients
			if len(bad_clients) > 0 {
				log.Debug("Cluster: Cleaning up dup and faulty clients:", len(bad_clients))
				for _, client := range bad_clients {
					client.Stop()
				}
			}

			// TODO: Check on remote locations nodes available to add
			room := max_clients - len(clients) + len(bad_clients)
			if room == 0 {
				// Seems we good for now, let's try in the next loop
				continue
			}

			if room < 0 {
				log.Debug("Cluster: Too much clients sitting in the roster:", -room)
				// Still not enough, so we can mark some good clients for removal in the next loop
				// TODO
			}

			if room > 0 {
				log.Debug("Cluster: Looking for the new candidates to add to the clients roster:", room)
				// We have some room - let's check the cluster nodes to connect with
				curr_node_loc := cl.fish.GetNode().LocationName

				// Request a list of cluster nodes
				nodes, err := cl.fish.NodeActiveList()
				if err != nil {
					log.Error("Cluster: Unable to get the active nodes list:", err)
					continue
				}

				// Randomize the nodes items to have variability between the cluster nodes
				rand.Shuffle(len(nodes), func(i, j int) { nodes[i], nodes[j] = nodes[j], nodes[i] })

				// Preparing the nodes list by location and filtering out the checked_nodes
				var sorted_nodes []*types.Node
				remote_location_client_num := 0
				for _, node := range nodes {
					// Looking for remote locations and checking the node is not in existing connections
					has_same_client := false
					for _, client := range clients {
						if client.Name() == node.Name {
							has_same_client = true
							// Checking node location of the client
							if node.LocationName != curr_node_loc {
								remote_location_client_num += 1
							}
							break
						}
					}
					if has_same_client {
						continue
					}

					// Making sure we do not bother this node too often
					if t, ok := checked_nodes[node.Name]; ok && t.Before(time.Now()) {
						// We already processed this node and it's not timed out yet
						continue
					}

					// Checking the node have the Address field properly set
					if node.Address == "" {
						continue
					}

					// Put the similar location nodes first, then remote location nodes
					if node.LocationName == curr_node_loc {
						sorted_nodes = append([]*types.Node{&node}, sorted_nodes...)
					} else {
						sorted_nodes = append(sorted_nodes, &node)
					}
				}

				// Reverse the sorted nodes to put remote locations first in case we need remotes
				// This will make us to use remote location clients in the first row
				if min_remote_loc_clients < remote_location_client_num {
					for i, j := 0, len(sorted_nodes)-1; i < j; i, j = i+1, j-1 {
						sorted_nodes[i], sorted_nodes[j] = sorted_nodes[j], sorted_nodes[i]
					}
				}

				for _, node := range sorted_nodes {
					log.Debug("Cluster: Available node to connect:", node.Name)
					// Check if this node is not curr location
					if node.LocationName != curr_node_loc {
						if min_remote_loc_clients >= remote_location_client_num {
							// Skipping since we've got enough remote location clients, so can focus
							// on the current location nodes
							continue
						}
						remote_location_client_num += 1
					}
					checked_nodes[node.Name] = time.Now().Add(30 * time.Minute)

					// Adding new client to the cluster
					cl.NewConnect(node.Address)
					room -= 1
					if room <= 0 {
						break
					}
				}
			}*/
		}
	}
}

// This function needed to periodically write the cluster info file, otherwise it will be written
// way too often and will make disk worn-out quicker then we would love to
func (cl *Cluster) infoUpdateProcess() {
	prev_time := cl.info.UpdatedAt
	update_ticker := time.NewTicker(time.Minute)
	for {
		if !cl.fish.IsRunning() {
			break
		}
		select {
		case <-update_ticker.C:
			// Write only when the cluster was updated since previous time
			if prev_time.Before(cl.info.UpdatedAt) {
				log.Debug("Cluster: update info")
				if err := cl.writeClusterInfo(); err != nil {
					log.Error("Cluster: Unable to write cluster info:", err)
				} else {
					prev_time = cl.info.UpdatedAt
				}
			}
		}
	}
}
