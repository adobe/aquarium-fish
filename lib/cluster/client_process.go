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
	"io"

	"github.com/google/uuid"

	"github.com/adobe/aquarium-fish/lib/cluster/msg"
	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/openapi/types"
)

// Starts the synchronization process with the remote cluster node
func (c *Client) syncRequest() {
	// Request all the cluster data since the last update
	// TODO: Add getting only the last changes by from data field
	c.Write(map[string]any{"type": "sync", "data": msg.Sync{}})
}

// Procesing incoming message
func (c *Client) processMessage(data []byte) {
	// Getting the type of the message
	var message msg.Message

	// Seems json.Unmarshal doesn't really like to be executed in parallel so using decoder
	dec := json.NewDecoder(bytes.NewReader(data))

	// Reading multiple messages that could potentially be joined
	for {
		if err := dec.Decode(&message); err == io.EOF {
			break
		} else if err != nil {
			log.Warnf("Cluster: Client %s: Unable to unmarshal the message container: %v", c.ident, err)
			return
		}

		// Processing the Message by type
		switch message.Type {
		case "cluster":
			// Received remote Cluster ID
			c.processCluster(&message)
		case "completed":
			// Long multi-message responce was completed on server side
			switch message.Resp {
			case "sync":
				log.Infof("Cluster: Client %s: Sync completed", c.ident)
				c.InSync = true
			default:
				log.Warnf("Cluster: Client %s: Unknown `completed` request for: %v", c.ident, message.Resp)
				return
			}
		case "sync":
			// Received sync request to send back the cluster DB
			c.processSync(&message)
		case "user":
			// Received DB users update
			c.processUsers(&message)
		case "label":
			// Received DB labels update
			c.processLabels(&message)
		case "application":
			// Received DB applications update
			c.processApplications(&message)
		case "application_state":
			// Received DB application states update
			c.processApplicationStates(&message)
		case "application_task":
			// Received DB application tasks update
			c.processApplicationTasks(&message)
		case "service_mapping":
			// Received DB service mappings update
			c.processServiceMappings(&message)
		case "vote":
			// Received DB votes update
			c.processVotes(&message)
		case "location":
			// Received DB locations update
			c.processLocations(&message)
		case "node":
			// Received DB nodes update
			c.processNodes(&message)
		default:
			log.Warnf("Cluster: Client %s: Unable to process the unknown message type: %s", c.ident, message.Type)
			return
		}
	}
}

func (c *Client) processSync(message *msg.Message) {
	log.Debugf("Cluster: Client %s: Received request for sync from: %v", c.ident, c.ws.RemoteAddr())

	var syncdata msg.Sync
	dec := json.NewDecoder(bytes.NewReader([]byte(message.Data)))
	if err := dec.Decode(&syncdata); err != nil {
		log.Warnf("Cluster: Client %s: Unable to unmarshal the Sync data: %v", c.ident, err)
		return
	}

	// TODO: Add filter from `syncdata` by updated_at / created_at to receive just the fresh stuff
	var filter *string

	// Sending back the cluster info
	{
		if err := c.Write(map[string]any{"resp": message.Type, "type": "cluster", "data": c.cluster.GetInfo()}); err != nil {
			log.Errorf("Cluster: Client %s: Unable to send users: %v", c.ident, err)
			return
		}
	}

	// Sending back the users
	{
		users, err := c.fish.UserFind(filter)
		if err != nil {
			log.Errorf("Cluster: Client %s: Unable to get Users to send: %v", c.ident, err)
			return
		}
		if len(users) > 0 {
			if err := c.Write(map[string]any{"resp": message.Type, "type": "user", "data": users}); err != nil {
				log.Errorf("Cluster: Client %s: Unable to send users: %v", c.ident, err)
				return
			}
		}
	}

	// Sending back the labels
	{
		labels, err := c.fish.LabelFind(filter)
		if err != nil {
			log.Errorf("Cluster: Client %s: Unable to get Labels to send: %v", c.ident, err)
			return
		}
		if len(labels) > 0 {
			if err := c.Write(map[string]any{"resp": message.Type, "type": "label", "data": labels}); err != nil {
				log.Errorf("Cluster: Client %s: Unable to send users: %v", c.ident, err)
				return
			}
		}
	}

	// Sending back the applications
	{
		applications, err := c.fish.ApplicationFind(filter)
		if err != nil {
			log.Errorf("Cluster: Client %s: Unable to get Applications to send: %v", c.ident, err)
			return
		}
		if len(applications) > 0 {
			if err := c.Write(map[string]any{"resp": message.Type, "type": "application", "data": applications}); err != nil {
				log.Errorf("Cluster: Client %s: Unable to send Applications: %v", c.ident, err)
				return
			}
		}
	}

	// Sending back the application states
	{
		application_states, err := c.fish.ApplicationStateFind(filter)
		if err != nil {
			log.Errorf("Cluster: Client %s: Unable to get ApplicationStates to send: %v", c.ident, err)
			return
		}
		if len(application_states) > 0 {
			if err := c.Write(map[string]any{"resp": message.Type, "type": "application_state", "data": application_states}); err != nil {
				log.Errorf("Cluster: Client %s: Unable to send ApplicationStates: %v", c.ident, err)
				return
			}
		}
	}

	// Sending back the application tasks
	{
		application_tasks, err := c.fish.ApplicationTaskFind(filter)
		if err != nil {
			log.Errorf("Cluster: Client %s: Unable to get ApplicationTasks to send: %v", c.ident, err)
			return
		}
		if len(application_tasks) > 0 {
			if err := c.Write(map[string]any{"resp": message.Type, "type": "application_task", "data": application_tasks}); err != nil {
				log.Errorf("Cluster: Client %s: Unable to send ApplicationTasks: %v", c.ident, err)
				return
			}
		}
	}

	// Sending back the service mappings
	{
		service_mappings, err := c.fish.ServiceMappingFind(filter)
		if err != nil {
			log.Errorf("Cluster: Client %s: Unable to get ServiceMappings to send: %v", c.ident, err)
			return
		}
		if len(service_mappings) > 0 {
			if err := c.Write(map[string]any{"resp": message.Type, "type": "service_mapping", "data": service_mappings}); err != nil {
				log.Errorf("Cluster: Client %s: Unable to send ServiceMappings: %v", c.ident, err)
				return
			}
		}
	}

	// Sending back the votes
	{
		// Votes really need to be sent only for the active applications
		votes, err := c.fish.VoteFind(filter)
		if err != nil {
			log.Errorf("Cluster: Client %s: Unable to get Votes to send: %v", c.ident, err)
			return
		}
		if len(votes) > 0 {
			if err := c.Write(map[string]any{"resp": message.Type, "type": "vote", "data": votes}); err != nil {
				log.Errorf("Cluster: Client %s: Unable to send Votes: %v", c.ident, err)
				return
			}
		}
	}

	// Sending back the locations
	{
		locations, err := c.fish.LocationFind(filter)
		if err != nil {
			log.Errorf("Cluster: Client %s: Unable to get Locations to send: %v", c.ident, err)
			return
		}
		if len(locations) > 0 {
			if err := c.Write(map[string]any{"resp": message.Type, "type": "location", "data": locations}); err != nil {
				log.Errorf("Cluster: Client %s: Unable to send Locations: %v", c.ident, err)
				return
			}
		}
	}

	// Sending back the nodes
	{
		nodes, err := c.fish.NodeFind(filter)
		if err != nil {
			log.Errorf("Cluster: Client %s: Unable to get Nodes to send: %v", c.ident, err)
			return
		}
		if len(nodes) > 0 {
			if err := c.Write(map[string]any{"resp": message.Type, "type": "node", "data": nodes}); err != nil {
				log.Errorf("Cluster: Client %s: Unable to send Nodes: %v", c.ident, err)
				return
			}
		}
	}

	// Sending back the sync completed message
	if err := c.Write(map[string]any{"resp": message.Type, "type": "completed"}); err != nil {
		log.Errorf("Cluster: Client %s: Unable to send sync completed message: %v", c.ident, err)
		return
	}
}

func (c *Client) processCluster(message *msg.Message) {
	var info ClusterInfo
	dec := json.NewDecoder(bytes.NewReader([]byte(message.Data)))
	if err := dec.Decode(&info); err != nil {
		log.Warnf("Cluster: Client %s: Unable to unmarshal the ClusterInfo data: %v", c.ident, err)
		return
	}

	log.Infof("Cluster: Client %s: Importing cluster: %s", c.ident, info.UID)
	if c.cluster.info.UID == uuid.Nil {
		// First time joining the cluster
		c.cluster.info.UID = info.UID
	} else if c.cluster.info.UID != info.UID {
		log.Warnf("Cluster: Client %s: Detected incorrect remote cluster: %s, %s != %s", c.ident, c.url, info.UID, c.cluster.info.UID)
		c.Valid = false
		return
	}

	c.Valid = true
}

func (c *Client) processUsers(message *msg.Message) {
	var items []types.User
	dec := json.NewDecoder(bytes.NewReader([]byte(message.Data)))
	if err := dec.Decode(&items); err != nil {
		log.Warnf("Cluster: Client %s: Unable to unmarshal the Users data: %v", c.ident, err)
		return
	}

	for _, i := range items {
		log.Debugf("Cluster: Client %s: Importing user: %v", c.ident, i.Name)
		if err := c.fish.UserImport(&i); err != nil {
			log.Warnf("Cluster: Client %s: Unable to import user '%s': %v", c.ident, i.Name, err)
		}
	}
}

func (c *Client) processLabels(message *msg.Message) {
	var items []types.Label
	dec := json.NewDecoder(bytes.NewReader([]byte(message.Data)))
	if err := dec.Decode(&items); err != nil {
		log.Warnf("Cluster: Client %s: Unable to unmarshal the Labels data: %v", c.ident, err)
		return
	}

	for _, i := range items {
		log.Debugf("Cluster: Client %s: Importing label: %s", c.ident, i.UID)
		if err := c.fish.LabelImport(&i); err != nil {
			log.Warnf("Cluster: Client %s: Unable to import label '%s': %v", c.ident, i.UID, err)
		}
	}
}

func (c *Client) processApplications(message *msg.Message) {
	var items []types.Application
	dec := json.NewDecoder(bytes.NewReader([]byte(message.Data)))
	if err := dec.Decode(&items); err != nil {
		log.Warnf("Cluster: Client %s: Unable to unmarshal the Applications data: %v", c.ident, err)
		return
	}

	for _, i := range items {
		log.Debugf("Cluster: Client %s: Importing application: %s", c.ident, i.UID)
		if err := c.fish.ApplicationImport(&i); err != nil {
			log.Warnf("Cluster: Client %s: Unable to import application '%s': %v", c.ident, i.UID, err)
		}
	}
}

func (c *Client) processApplicationStates(message *msg.Message) {
	var items []types.ApplicationState
	dec := json.NewDecoder(bytes.NewReader([]byte(message.Data)))
	if err := dec.Decode(&items); err != nil {
		log.Warnf("Cluster: Client %s: Unable to unmarshal the ApplicationStates data: %v", c.ident, err)
		return
	}

	for _, i := range items {
		log.Debugf("Cluster: Client %s: Importing application state: %s", c.ident, i.UID)
		if err := c.fish.ApplicationStateImport(&i); err != nil {
			log.Warnf("Cluster: Client %s: Unable to import application state '%s': %v", c.ident, i.UID, err)
		}
	}
}

func (c *Client) processApplicationTasks(message *msg.Message) {
	var items []types.ApplicationTask
	dec := json.NewDecoder(bytes.NewReader([]byte(message.Data)))
	if err := dec.Decode(&items); err != nil {
		log.Warnf("Cluster: Client %s: Unable to unmarshal the ApplicationTasks data: %v", c.ident, err)
		return
	}

	for _, i := range items {
		log.Debugf("Cluster: Client %s: Importing application task: %s", c.ident, i.UID)
		if err := c.fish.ApplicationTaskImport(&i); err != nil {
			log.Warnf("Cluster: Client %s: Unable to import application task '%s': %v", c.ident, i.UID, err)
		}
	}
}

func (c *Client) processServiceMappings(message *msg.Message) {
	var items []types.ServiceMapping
	dec := json.NewDecoder(bytes.NewReader([]byte(message.Data)))
	if err := dec.Decode(&items); err != nil {
		log.Warnf("Cluster: Client %s: Unable to unmarshal the ServiceMappings data: %v", c.ident, err)
		return
	}

	for _, i := range items {
		log.Debugf("Cluster: Client %s: Importing service mapping: %s", c.ident, i.UID)
		if err := c.fish.ServiceMappingImport(&i); err != nil {
			log.Warnf("Cluster: Client %s: Unable to import service mapping '%s': %v", c.ident, i.UID, err)
		}
	}
}

func (c *Client) processVotes(message *msg.Message) {
	var items []types.Vote
	dec := json.NewDecoder(bytes.NewReader([]byte(message.Data)))
	if err := dec.Decode(&items); err != nil {
		log.Warnf("Cluster: Client %s: Unable to unmarshal the Votes data: %v", c.ident, err)
		return
	}

	for _, i := range items {
		log.Debugf("Cluster: Client %s: Importing vote: %s", c.ident, i.UID)
		if err := c.fish.VoteImport(&i); err != nil {
			log.Warnf("Cluster: Client %s: Unable to import vote '%v': %v", c.ident, i.UID, err)
		}
	}
}

func (c *Client) processLocations(message *msg.Message) {
	var items []types.Location
	dec := json.NewDecoder(bytes.NewReader([]byte(message.Data)))
	if err := dec.Decode(&items); err != nil {
		log.Warnf("Cluster: Client %s: Unable to unmarshal the Locations data: %v", c.ident, err)
		return
	}

	for _, i := range items {
		log.Debugf("Cluster: Client %s: Importing location: %s", c.ident, i.Name)
		if err := c.fish.LocationImport(&i); err != nil {
			log.Warnf("Cluster: Client %s: Unable to import location '%s': %v", c.ident, i.Name, err)
		}
	}
}

func (c *Client) processNodes(message *msg.Message) {
	var items []types.Node
	dec := json.NewDecoder(bytes.NewReader([]byte(message.Data)))
	if err := dec.Decode(&items); err != nil {
		log.Warnf("Cluster: Client %s: Unable to unmarshal the Nodes data: %v", c.ident, err)
		return
	}

	for _, i := range items {
		log.Debugf("Cluster: Client %s: Importing node: %s", c.ident, i.UID)
		if err := c.fish.NodeImport(&i); err != nil {
			log.Warnf("Cluster: Client %s: Unable to import node '%s': %v", c.ident, i.UID, err)
		}
	}
}
