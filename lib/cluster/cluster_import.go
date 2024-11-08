/**
 * Copyright 2024 Adobe. All rights reserved.
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

	"github.com/adobe/aquarium-fish/lib/cluster/msg"
	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/openapi/types"
)

func (cl *Cluster) importUser(message *msg.Message) {
	var items []types.User
	dec := json.NewDecoder(bytes.NewReader([]byte(message.Data)))
	if err := dec.Decode(&items); err != nil {
		log.Warn("Cluster: Unable to unmarshal the Users data:", err)
		return
	}

	for _, i := range items {
		log.Debug("Cluster: Importing User:", i.Name)
		if err := cl.fish.UserImport(&i); err != nil {
			log.Warnf("Cluster: Unable to import user '%s': %v", i.Name, err)
		}
	}
}

func (cl *Cluster) importLabel(message *msg.Message) {
	var items []types.Label
	dec := json.NewDecoder(bytes.NewReader([]byte(message.Data)))
	if err := dec.Decode(&items); err != nil {
		log.Warn("Cluster: Unable to unmarshal the Labels data:", err)
		return
	}

	for _, i := range items {
		log.Debug("Cluster: Importing Label:", i.UID)
		if err := cl.fish.LabelImport(&i); err != nil {
			log.Warnf("Cluster: Unable to import label '%s': %v", i.UID, err)
		}
	}
}

func (cl *Cluster) importApplication(message *msg.Message) {
	var items []types.Application
	dec := json.NewDecoder(bytes.NewReader([]byte(message.Data)))
	if err := dec.Decode(&items); err != nil {
		log.Warn("Cluster: Unable to unmarshal the Applications data:", err)
		return
	}

	for _, i := range items {
		log.Debug("Cluster: Importing Application:", i.UID)
		if err := cl.fish.ApplicationImport(&i); err != nil {
			log.Warnf("Cluster: Unable to import application '%s': %v", i.UID, err)
		}
	}
}

func (cl *Cluster) importApplicationState(message *msg.Message) {
	var items []types.ApplicationState
	dec := json.NewDecoder(bytes.NewReader([]byte(message.Data)))
	if err := dec.Decode(&items); err != nil {
		log.Warn("Cluster: Unable to unmarshal the ApplicationStates data:", err)
		return
	}

	for _, i := range items {
		log.Debug("Cluster: Importing ApplicationState:", i.UID)
		if err := cl.fish.ApplicationStateImport(&i); err != nil {
			log.Warnf("Cluster: Unable to import application state '%s': %v", i.UID, err)
		}
	}
}

func (cl *Cluster) importApplicationTask(message *msg.Message) {
	var items []types.ApplicationTask
	dec := json.NewDecoder(bytes.NewReader([]byte(message.Data)))
	if err := dec.Decode(&items); err != nil {
		log.Warn("Cluster: Unable to unmarshal the ApplicationTasks data:", err)
		return
	}

	for _, i := range items {
		log.Debug("Cluster: Importing ApplicationTask:", i.UID)
		if err := cl.fish.ApplicationTaskImport(&i); err != nil {
			log.Warnf("Cluster: Unable to import application task '%s': %v", i.UID, err)
		}
	}
}

func (cl *Cluster) importServiceMapping(message *msg.Message) {
	var items []types.ServiceMapping
	dec := json.NewDecoder(bytes.NewReader([]byte(message.Data)))
	if err := dec.Decode(&items); err != nil {
		log.Warn("Cluster: Unable to unmarshal the ServiceMappings data:", err)
		return
	}

	for _, i := range items {
		log.Debug("Cluster: Importing ServiceMapping:", i.UID)
		if err := cl.fish.ServiceMappingImport(&i); err != nil {
			log.Warnf("Cluster: Unable to import service mapping '%s': %v", i.UID, err)
		}
	}
}

// importVote is a special one - it doesn't import into DB, but instead to memory storage of Fish
func (cl *Cluster) importVote(message *msg.Message) {
	var items []types.Vote
	dec := json.NewDecoder(bytes.NewReader([]byte(message.Data)))
	if err := dec.Decode(&items); err != nil {
		log.Warn("Cluster: Unable to unmarshal the Votes data:", err)
		return
	}

	log.Debug("Cluster: Importing Votes amount:", len(items))
	cl.fish.StorageVotesAdd(items)
}

func (cl *Cluster) importLocation(message *msg.Message) {
	var items []types.Location
	dec := json.NewDecoder(bytes.NewReader([]byte(message.Data)))
	if err := dec.Decode(&items); err != nil {
		log.Warn("Cluster: Unable to unmarshal the Locations data:", err)
		return
	}

	for _, i := range items {
		log.Debug("Cluster: Importing Location:", i.Name)
		if err := cl.fish.LocationImport(&i); err != nil {
			log.Warnf("Cluster: Unable to import location '%s': %v", i.Name, err)
		}
	}
}

func (cl *Cluster) importNode(message *msg.Message) {
	var items []types.Node
	dec := json.NewDecoder(bytes.NewReader([]byte(message.Data)))
	if err := dec.Decode(&items); err != nil {
		log.Warn("Cluster: Unable to unmarshal the Nodes data:", err)
		return
	}

	for _, i := range items {
		log.Debug("Cluster: Importing Node:", i.UID)
		if err := cl.fish.NodeImport(&i); err != nil {
			log.Warnf("Cluster: Unable to import node '%s': %v", i.UID, err)
		}
	}
}

func (cl *Cluster) importResource(message *msg.Message) {
	var items []types.Resource
	dec := json.NewDecoder(bytes.NewReader([]byte(message.Data)))
	if err := dec.Decode(&items); err != nil {
		log.Warn("Cluster: Unable to unmarshal the Resources data:", err)
		return
	}

	for _, i := range items {
		log.Debug("Cluster: Importing Resource:", i.UID)
		if err := cl.fish.ResourceImport(&i); err != nil {
			log.Warnf("Cluster: Unable to import resource '%s': %v", i.UID, err)
		}
	}
}
