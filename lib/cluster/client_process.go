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
	"bytes"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/adobe/aquarium-fish/lib/cluster/msg"
	"github.com/adobe/aquarium-fish/lib/log"
)

// Starts the synchronization process with the remote cluster node
func (c *Client) SyncRequest() {
	// Check the sync process is not running and client is not in sync already
	if _, ok := c.long_ops["sync"]; ok || c.cluster.InSync {
		return
	}
	c.long_ops["sync"] = &WaitGroupCount{}

	// Sending the Node object belongs to this node
	nodes := []any{c.fish.GetNode()}
	if err := c.Write(msg.NewMessage("Node", "", nodes)); err != nil {
		log.Errorf("Cluster: Client %s: Unable to send self Node: %v", c.Ident(), err)
	}

	// Request all the cluster data since the last update
	// TODO: Make the received messages to update the cluster UpdatedAt field
	c.Write(msg.NewMessage("sync", "", msg.Sync{From: c.cluster.info.UpdatedAt}))
}

// Procesing incoming message
func (c *Client) processMessage(message msg.Message) {
	// Pre-processing the long-running responses
	if message.Resp != "" && message.Type != "completed" {
		c.long_ops[message.Resp].Add(1)
	}

	// Processing the Message by type
	switch message.Type {
	case "cluster":
		// Received remote Cluster ID
		c.processCluster(&message)
	case "completed":
		// Received completed message of the long-running responses
		c.processCompleted(&message)
	case "sync":
		// Received sync request to send back the cluster DB
		c.processSync(&message)
	default:
		// Here we processing the DB objects transfer and broadcast them to the other conected nodes

		// Processing the DB object if allowed
		if importFunc, ok := c.cluster.ImportTypeAllowed(message.Type); ok {
			// Broadcast only when the client in sync
			if c.cluster.InSync {
				// Check the message duplication in client
				if ok := c.processed_sums.Put(message.Sum); !ok {
					// The message is already known by the client
					break
				}
				// Try to broadcast the message and check if it's already processed by the cluster
				if err := c.cluster.Send(&message); err != nil {
					// The message is already processed by the cluster or error happened
					break
				}
			}
			importFunc(&message)
		} else {
			log.Warnf("Cluster: Client %s: Unable to process the unknown message type: %s", c.Ident(), message.Type)
		}
	}

	// Post-processing the long-running processes
	if message.Resp != "" && message.Type != "completed" {
		c.long_ops[message.Resp].Done()
	}
}

func (c *Client) processCluster(message *msg.Message) {
	var info ClusterInfo
	dec := json.NewDecoder(bytes.NewReader([]byte(message.Data)))
	if err := dec.Decode(&info); err != nil {
		log.Warnf("Cluster: Client %s: Unable to unmarshal the ClusterInfo data: %v", c.Ident(), err)
		return
	}

	log.Infof("Cluster: Client %s: Importing cluster: %s", c.Ident(), info.UID)
	if c.cluster.info.UID == uuid.Nil {
		// First time joining the cluster
		c.cluster.info.UID = info.UID
	} else if c.cluster.info.UID != info.UID {
		c.Stop()
		c.ConnFail = log.Errorf("Cluster: Client %s: Detected incorrect remote cluster: %s, %s != %s", c.Ident(), c.url.String(), info.UID, c.cluster.info.UID)
		return
	}
}

func (c *Client) processCompleted(message *msg.Message) {
	// Long multi-message responce was completed on server side
	// Read the counter of sent packages to wait for all the packages processing
	var count int
	dec := json.NewDecoder(bytes.NewReader([]byte(message.Data)))
	if err := dec.Decode(&count); err != nil {
		log.Warnf("Cluster: Client %s: Unable to unmarshal the Completed data: %v", c.Ident(), err)
		return
	}

	for c.long_ops[message.Resp].GetCount() != count {
		time.Sleep(time.Second / 4)
	}

	// Wait for the processing actually completed
	c.long_ops[message.Resp].Wait()

	// Processing the specific completed operations
	switch message.Resp {
	case "sync":
		log.Infof("Cluster: Client %s: Sync completed", c.Ident())
		delete(c.long_ops, "sync")
	default:
		log.Warnf("Cluster: Client %s: Unknown `completed` request for: %v", c.Ident(), message.Resp)
		return
	}
}

func (c *Client) processSync(message *msg.Message) {
	log.Debugf("Cluster: Client %s: Received request for sync from: %v", c.Ident(), c.ws.RemoteAddr())

	var syncdata msg.Sync
	dec := json.NewDecoder(bytes.NewReader([]byte(message.Data)))
	if err := dec.Decode(&syncdata); err != nil {
		log.Warnf("Cluster: Client %s: Unable to unmarshal the Sync data: %v", c.Ident(), err)
		return
	}

	// TODO: Add filter from `syncdata` by updated_at / created_at to receive just the fresh stuff
	var filter *string

	// Amount of sent packets to wait for completed on sync complete
	counter := 0

	// Sending back the cluster info
	{
		//log.Debugf("Cluster: Client %s: Sending cluster", c.Ident())
		counter += 1
		if err := c.Write(msg.NewMessage("cluster", message.Type, c.cluster.GetInfo())); err != nil {
			log.Errorf("Cluster: Client %s: Unable to send users: %v", c.Ident(), err)
			return
		}
	}

	// Sending back the users
	{
		//log.Debugf("Cluster: Client %s: Sending users", c.Ident())
		users, err := c.fish.UserFind(filter)
		if err != nil {
			log.Errorf("Cluster: Client %s: Unable to get Users to send: %v", c.Ident(), err)
			return
		}
		if len(users) > 0 {
			counter += 1
			if err := c.Write(msg.NewMessage("User", message.Type, users)); err != nil {
				log.Errorf("Cluster: Client %s: Unable to send users: %v", c.Ident(), err)
				return
			}
		}
	}

	// Sending back the labels
	{
		//log.Debugf("Cluster: Client %s: Sending labels", c.Ident())
		labels, err := c.fish.LabelFind(filter)
		if err != nil {
			log.Errorf("Cluster: Client %s: Unable to get Labels to send: %v", c.Ident(), err)
			return
		}
		if len(labels) > 0 {
			counter += 1
			if err := c.Write(msg.NewMessage("Label", message.Type, labels)); err != nil {
				log.Errorf("Cluster: Client %s: Unable to send users: %v", c.Ident(), err)
				return
			}
		}
	}

	// Sending back the applications
	{
		//log.Debugf("Cluster: Client %s: Sending applications", c.Ident())
		applications, err := c.fish.ApplicationFind(filter)
		if err != nil {
			log.Errorf("Cluster: Client %s: Unable to get Applications to send: %v", c.Ident(), err)
			return
		}
		if len(applications) > 0 {
			counter += 1
			if err := c.Write(msg.NewMessage("Application", message.Type, applications)); err != nil {
				log.Errorf("Cluster: Client %s: Unable to send Applications: %v", c.Ident(), err)
				return
			}
		}
	}

	// Sending back the application states
	{
		//log.Debugf("Cluster: Client %s: Sending application states", c.Ident())
		application_states, err := c.fish.ApplicationStateFind(filter)
		if err != nil {
			log.Errorf("Cluster: Client %s: Unable to get ApplicationStates to send: %v", c.Ident(), err)
			return
		}
		if len(application_states) > 0 {
			counter += 1
			if err := c.Write(msg.NewMessage("ApplicationState", message.Type, application_states)); err != nil {
				log.Errorf("Cluster: Client %s: Unable to send ApplicationStates: %v", c.Ident(), err)
				return
			}
		}
	}

	// Sending back the application tasks
	{
		//log.Debugf("Cluster: Client %s: Sending application tasks", c.Ident())
		application_tasks, err := c.fish.ApplicationTaskFind(filter)
		if err != nil {
			log.Errorf("Cluster: Client %s: Unable to get ApplicationTasks to send: %v", c.Ident(), err)
			return
		}
		if len(application_tasks) > 0 {
			counter += 1
			if err := c.Write(msg.NewMessage("ApplicationTask", message.Type, application_tasks)); err != nil {
				log.Errorf("Cluster: Client %s: Unable to send ApplicationTasks: %v", c.Ident(), err)
				return
			}
		}
	}

	// Sending back the service mappings
	{
		//log.Debugf("Cluster: Client %s: Sending service mappings", c.Ident())
		service_mappings, err := c.fish.ServiceMappingFind(filter)
		if err != nil {
			log.Errorf("Cluster: Client %s: Unable to get ServiceMappings to send: %v", c.Ident(), err)
			return
		}
		if len(service_mappings) > 0 {
			counter += 1
			if err := c.Write(msg.NewMessage("ServiceMapping", message.Type, service_mappings)); err != nil {
				log.Errorf("Cluster: Client %s: Unable to send ServiceMappings: %v", c.Ident(), err)
				return
			}
		}
	}

	// Sending back the votes
	{
		//log.Debugf("Cluster: Client %s: Sending votes", c.Ident())
		// Votes really need to be sent only for the active applications
		votes, err := c.fish.VoteFind(filter)
		if err != nil {
			log.Errorf("Cluster: Client %s: Unable to get Votes to send: %v", c.Ident(), err)
			return
		}
		if len(votes) > 0 {
			counter += 1
			if err := c.Write(msg.NewMessage("Vote", message.Type, votes)); err != nil {
				log.Errorf("Cluster: Client %s: Unable to send Votes: %v", c.Ident(), err)
				return
			}
		}
	}

	// Sending back the locations
	{
		//log.Debugf("Cluster: Client %s: Sending locations", c.Ident())
		locations, err := c.fish.LocationFind(filter)
		if err != nil {
			log.Errorf("Cluster: Client %s: Unable to get Locations to send: %v", c.Ident(), err)
			return
		}
		if len(locations) > 0 {
			counter += 1
			if err := c.Write(msg.NewMessage("Location", message.Type, locations)); err != nil {
				log.Errorf("Cluster: Client %s: Unable to send Locations: %v", c.Ident(), err)
				return
			}
		}
	}

	// Sending back the nodes
	{
		//log.Debugf("Cluster: Client %s: Sending nodes", c.Ident())
		nodes, err := c.fish.NodeFind(filter)
		if err != nil {
			log.Errorf("Cluster: Client %s: Unable to get Nodes to send: %v", c.Ident(), err)
			return
		}
		if len(nodes) > 0 {
			counter += 1
			if err := c.Write(msg.NewMessage("Node", message.Type, nodes)); err != nil {
				log.Errorf("Cluster: Client %s: Unable to send Nodes: %v", c.Ident(), err)
				return
			}
		}
	}

	// Sending back the resources
	{
		//log.Debugf("Cluster: Client %s: Sending resources", c.Ident())
		resources, err := c.fish.ResourceFind(filter)
		if err != nil {
			log.Errorf("Cluster: Client %s: Unable to get Resources to send: %v", c.Ident(), err)
			return
		}
		if len(resources) > 0 {
			counter += 1
			if err := c.Write(msg.NewMessage("Resource", message.Type, resources)); err != nil {
				log.Errorf("Cluster: Client %s: Unable to send Resources: %v", c.Ident(), err)
				return
			}
		}
	}

	// Sending back the sync completed message
	//log.Debugf("Cluster: Client %s: Sending sync completed", c.Ident())
	if err := c.Write(msg.NewMessage("completed", message.Type, counter)); err != nil {
		log.Errorf("Cluster: Client %s: Unable to send sync completed message: %v", c.Ident(), err)
		return
	}
}
