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
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/fasthttp/websocket"

	"github.com/adobe/aquarium-fish/lib/cluster/msg"
	"github.com/adobe/aquarium-fish/lib/fish"
	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/openapi/types"
)

// Send pings to peer with this period
const ping_period = 30 * time.Second

type ClusterClient struct {
	fish      *fish.Fish
	url       url.URL
	send_buf  chan []byte
	ctx       context.Context
	ctxCancel context.CancelFunc

	mu sync.RWMutex
	ws *websocket.Conn

	cluster *Cluster
}

func (c *ClusterClient) Connect() *websocket.Conn {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ws != nil {
		return c.ws
	}

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for ; ; <-ticker.C {
		select {
		case <-c.ctx.Done():
			return nil
		default:
			config := &tls.Config{
				RootCAs:      c.cluster.ca_pool,
				Certificates: []tls.Certificate{c.cluster.certkey},
			}
			dialer := &websocket.Dialer{
				Proxy:             http.ProxyFromEnvironment,
				HandshakeTimeout:  45 * time.Second,
				TLSClientConfig:   config,
				EnableCompression: true,
			}
			ws, _, err := dialer.Dial(c.url.String(), nil)
			if err != nil {
				log.Errorf("ClusterClient %s: Cannot connect to websocket: %s: %v", c.url.Host, c.url.String(), err)
				continue
			}

			log.Infof("ClusterClient %s: Connected to node", c.url.Host)
			c.ws = ws

			// The node is connected to the cluster - so starting the sync process to ensure the
			// node is up to date (TODO: before interacting with the cluster)
			go c.syncProcess()

			return c.ws
		}
	}
}

func (c *ClusterClient) listen() {
	log.Infof("ClusterClient %s: Listen for the messages", c.url.Host)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			for {
				ws := c.Connect()
				if ws == nil {
					return
				}
				_, message, err := ws.ReadMessage()
				if err != nil {
					log.Errorf("ClusterClient %s: Cannot read websocket message: %v", c.url.Host, err)
					c.closeWs()
					break
				}
				//log.Debugf("ClusterClient %s: Received msg: %s", c.url.Host, message)
				go c.processMessage(message)
			}
		}
	}
}

// Write data to the websocket server
func (c *ClusterClient) Write(payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*50)
	defer cancel()

	for {
		select {
		case c.send_buf <- data:
			return nil
		case <-ctx.Done():
			return fmt.Errorf("context canceled")
		}
	}
}

// Function to write the requested data into the socket
func (c *ClusterClient) listenWrite() {
	for data := range c.send_buf {
		ws := c.Connect()
		if ws == nil {
			log.Errorf("ClusterClient %s: No websocket connection: %v", c.url.Host, fmt.Errorf("ws is nil"))
			continue
		}

		if err := ws.WriteMessage(
			websocket.TextMessage,
			data,
		); err != nil {
			log.Errorf("ClusterClient %s: Write error: %v", c.url.Host, err)
		}
	}
}

// Close will send close message and shutdown websocket connection
func (c *ClusterClient) Stop() {
	c.ctxCancel()
	c.closeWs()
}

// Close will send close message and shutdown websocket connection
func (c *ClusterClient) closeWs() {
	c.mu.Lock()
	if c.ws != nil {
		c.ws.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		c.ws.Close()
		c.ws = nil
	}
	c.mu.Unlock()
}

// Ensures the socket is active and not silently dropped
func (c *ClusterClient) ping() {
	log.Infof("ClusterClient %s: Ping started", c.url.Host)
	ticker := time.NewTicker(ping_period)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			ws := c.Connect()
			if ws == nil {
				continue
			}
			if err := c.ws.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(ping_period/2)); err != nil {
				c.closeWs()
			}
		case <-c.ctx.Done():
			return
		}
	}
}

// Starts the synchronization process with the remote cluster node
func (c *ClusterClient) syncProcess() {
	// Request all the cluster data since the last update
	// TODO: Add getting only the last changes by from data field
	c.Write(msg.Message{Type: "sync"})
}

func (c *ClusterClient) processMessage(data []byte) {
	// Getting the type of the message
	var message msg.Message
	// Seems json.Unmarshal doesn't really like to be executed in parallel so using decoder
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&message); err != nil {
		log.Warn("Cluster: Client: Unable to unmarshal the message container:", err)
		return
	}

	// Processing the Message by type
	switch message.Type {
	case "user":
		c.processUsers([]byte(message.Data))
	case "label":
		c.processLabels([]byte(message.Data))
	case "application":
		c.processApplications([]byte(message.Data))
	case "application_state":
		c.processApplicationStates([]byte(message.Data))
	case "application_task":
		c.processApplicationTasks([]byte(message.Data))
	case "service_mapping":
		c.processServiceMappings([]byte(message.Data))
	case "vote":
		c.processVotes([]byte(message.Data))
	case "location":
		c.processLocations([]byte(message.Data))
	case "node":
		c.processNodes([]byte(message.Data))
	default:
		log.Warn("Cluster: Client: Unable to process the unknown message type:", message.Type)
		return
	}
}

func (c *ClusterClient) processUsers(data []byte) {
	var items []types.User
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&items); err != nil {
		log.Warn("Cluster: Client: Unable to unmarshal the Users data:", err)
		return
	}

	for _, i := range items {
		log.Debug("Importing user:", i.Name)
		if err := c.fish.UserImport(&i); err != nil {
			log.Warn("Unable to import user:", i.Name, err)
		}
	}
}

func (c *ClusterClient) processLabels(data []byte) {
	var items []types.Label
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&items); err != nil {
		log.Warn("Cluster: Client: Unable to unmarshal the Labels data:", err)
		return
	}

	for _, i := range items {
		log.Debug("Importing label:", i.UID)
		if err := c.fish.LabelImport(&i); err != nil {
			log.Warn("Unable to import label:", i.UID, err)
		}
	}
}

func (c *ClusterClient) processApplications(data []byte) {
	var items []types.Application
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&items); err != nil {
		log.Warn("Cluster: Client: Unable to unmarshal the Applications data:", err)
		return
	}

	for _, i := range items {
		log.Debug("Importing application:", i.UID)
		if err := c.fish.ApplicationImport(&i); err != nil {
			log.Warn("Unable to import application:", i.UID, err)
		}
	}
}

func (c *ClusterClient) processApplicationStates(data []byte) {
	var items []types.ApplicationState
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&items); err != nil {
		log.Warn("Cluster: Client: Unable to unmarshal the ApplicationStates data:", err)
		return
	}

	for _, i := range items {
		log.Debug("Importing application state:", i.UID)
		if err := c.fish.ApplicationStateImport(&i); err != nil {
			log.Warn("Unable to import application state:", i.UID, err)
		}
	}
}

func (c *ClusterClient) processApplicationTasks(data []byte) {
	var items []types.ApplicationTask
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&items); err != nil {
		log.Warn("Cluster: Client: Unable to unmarshal the ApplicationTasks data:", err)
		return
	}

	for _, i := range items {
		log.Debug("Importing application task:", i.UID)
		if err := c.fish.ApplicationTaskImport(&i); err != nil {
			log.Warn("Unable to import application task:", i.UID, err)
		}
	}
}

func (c *ClusterClient) processServiceMappings(data []byte) {
	var items []types.ServiceMapping
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&items); err != nil {
		log.Warn("Cluster: Client: Unable to unmarshal the ServiceMappings data:", err)
		return
	}

	for _, i := range items {
		log.Debug("Importing service mapping:", i.UID)
		if err := c.fish.ServiceMappingImport(&i); err != nil {
			log.Warn("Unable to import service mapping:", i.UID, err)
		}
	}
}

func (c *ClusterClient) processVotes(data []byte) {
	var items []types.Vote
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&items); err != nil {
		log.Warn("Cluster: Client: Unable to unmarshal the Votes data:", err)
		return
	}

	for _, i := range items {
		log.Debug("Importing vote:", i.UID)
		if err := c.fish.VoteImport(&i); err != nil {
			log.Warn("Unable to import vote:", i.UID, err)
		}
	}
}

func (c *ClusterClient) processLocations(data []byte) {
	var items []types.Location
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&items); err != nil {
		log.Warn("Cluster: Client: Unable to unmarshal the Locations data:", err)
		return
	}

	for _, i := range items {
		log.Debug("Importing location:", i.Name)
		if err := c.fish.LocationImport(&i); err != nil {
			log.Warn("Unable to import location:", i.Name, err)
		}
	}
}

func (c *ClusterClient) processNodes(data []byte) {
	var items []types.Node
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&items); err != nil {
		log.Warn("Cluster: Client: Unable to unmarshal the Nodes data:", err)
		return
	}

	for _, i := range items {
		log.Debug("Importing node:", i.UID)
		if err := c.fish.NodeImport(&i); err != nil {
			log.Warn("Unable to import node:", i.UID, err)
		}
	}
}
