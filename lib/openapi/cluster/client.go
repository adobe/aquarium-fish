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
	"encoding/json"
	"fmt"
	"time"

	"github.com/fasthttp/websocket"

	"github.com/adobe/aquarium-fish/lib/cluster/msg"
	"github.com/adobe/aquarium-fish/lib/fish"
	"github.com/adobe/aquarium-fish/lib/log"
)

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer.
	maxMessageSize = 512
)

var (
	newline = []byte{'\n'}
	space   = []byte{' '}
)

// Client is a middleman between the websocket connection and the hub.
type Client struct {
	fish *fish.Fish
	hub  *Hub

	// The websocket connection.
	conn *websocket.Conn

	// Buffered channel of outbound messages.
	send chan []byte
}

// readPump pumps messages from the websocket connection to the hub.
//
// The application runs readPump in a per-connection goroutine. The application
// ensures that there is at most one reader on a connection by executing all
// reads from this goroutine.
func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()
	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error { c.conn.SetReadDeadline(time.Now().Add(pongWait)); return nil })
	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Warnf("Cluster: Client %v readPump: reading error", c.conn.RemoteAddr())
			}
			break
		}
		message = bytes.TrimSpace(bytes.Replace(message, newline, space, -1))
		//log.Debugf("Cluster: Client %v readPump: got: %s", c.conn.RemoteAddr(), message)
		go c.processMessage(message)
	}
}

func (c *Client) processMessage(data []byte) {
	// Getting the type of the message
	var message msg.Message
	if err := json.Unmarshal(data, &message); err != nil {
		log.Warn("Cluster: Client: Unable to unmarshal the message container:", err)
		return
	}

	// Processing the Message by type
	switch message.Type {
	case "sync":
		log.Debug("Cluster: Client: Received request for sync from:", c.conn.RemoteAddr())

		// TODO: Add filter by updated_at / created_at to receive just the fresh stuff
		var filter *string

		// Sending back the users
		{
			users, err := c.fish.UserFind(filter)
			if err != nil {
				log.Error("Cluster: Client: Unable to get Users to send:", err)
				return
			}
			if len(users) > 0 {
				if err := c.Write(map[string]any{"type": "user", "data": users}); err != nil {
					log.Error("Cluster: Client: Unable to send users:", err)
					return
				}
			}
		}

		// Sending back the labels
		{
			labels, err := c.fish.LabelFind(filter)
			if err != nil {
				log.Error("Cluster: Client: Unable to get Labels to send:", err)
				return
			}
			if len(labels) > 0 {
				if err := c.Write(map[string]any{"type": "label", "data": labels}); err != nil {
					log.Error("Cluster: Client: Unable to send users:", err)
					return
				}
			}
		}

		// Sending back the applications
		{
			applications, err := c.fish.ApplicationFind(filter)
			if err != nil {
				log.Error("Cluster: Client: Unable to get Applications to send:", err)
				return
			}
			if len(applications) > 0 {
				if err := c.Write(map[string]any{"type": "application", "data": applications}); err != nil {
					log.Error("Cluster: Client: Unable to send Applications:", err)
					return
				}
			}
		}

		// Sending back the application states
		{
			application_states, err := c.fish.ApplicationStateFind(filter)
			if err != nil {
				log.Error("Cluster: Client: Unable to get ApplicationStates to send:", err)
				return
			}
			if len(application_states) > 0 {
				if err := c.Write(map[string]any{"type": "application_state", "data": application_states}); err != nil {
					log.Error("Cluster: Client: Unable to send ApplicationStates:", err)
					return
				}
			}
		}

		// Sending back the application tasks
		{
			application_tasks, err := c.fish.ApplicationTaskFind(filter)
			if err != nil {
				log.Error("Cluster: Client: Unable to get ApplicationTasks to send:", err)
				return
			}
			if len(application_tasks) > 0 {
				if err := c.Write(map[string]any{"type": "application_task", "data": application_tasks}); err != nil {
					log.Error("Cluster: Client: Unable to send ApplicationTasks:", err)
					return
				}
			}
		}

		// Sending back the service mappings
		{
			service_mappings, err := c.fish.ServiceMappingFind(filter)
			if err != nil {
				log.Error("Cluster: Client: Unable to get ServiceMappings to send:", err)
				return
			}
			if len(service_mappings) > 0 {
				if err := c.Write(map[string]any{"type": "service_mapping", "data": service_mappings}); err != nil {
					log.Error("Cluster: Client: Unable to send ServiceMappings:", err)
					return
				}
			}
		}

		// Sending back the votes
		{
			// Votes really need to be sent only for the active applications
			votes, err := c.fish.VoteFind(filter)
			if err != nil {
				log.Error("Cluster: Client: Unable to get Votes to send:", err)
				return
			}
			if len(votes) > 0 {
				if err := c.Write(map[string]any{"type": "vote", "data": votes}); err != nil {
					log.Error("Cluster: Client: Unable to send Votes:", err)
					return
				}
			}
		}

		// Sending back the locations
		{
			locations, err := c.fish.LocationFind(filter)
			if err != nil {
				log.Error("Cluster: Client: Unable to get Locations to send:", err)
				return
			}
			if len(locations) > 0 {
				if err := c.Write(map[string]any{"type": "location", "data": locations}); err != nil {
					log.Error("Cluster: Client: Unable to send Locations:", err)
					return
				}
			}
		}

		// Sending back the nodes
		{
			nodes, err := c.fish.NodeFind(filter)
			if err != nil {
				log.Error("Cluster: Client: Unable to get Nodes to send:", err)
				return
			}
			if len(nodes) > 0 {
				if err := c.Write(map[string]any{"type": "node", "data": nodes}); err != nil {
					log.Error("Cluster: Client: Unable to send Nodes:", err)
					return
				}
			}
		}
	default:
		log.Warn("Cluster: Client: Unable to process the unknown message type:", message.Type)
		return
	}
}

// Write data to the websocket client
func (c *Client) Write(payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*100)
	defer cancel()

	for {
		select {
		case c.send <- data:
			return nil
		case <-ctx.Done():
			return fmt.Errorf("context canceled")
		}
	}
}

// writePump pumps messages from the hub to the websocket connection.
//
// A goroutine running writePump is started for each connection. The
// application ensures that there is at most one writer to a connection by
// executing all writes from this goroutine.
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()
	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// The hub closed the channel.
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			// Add queued chat messages to the current websocket message.
			n := len(c.send)
			for i := 0; i < n; i++ {
				w.Write(newline)
				w.Write(<-c.send)
			}

			if err := w.Close(); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
