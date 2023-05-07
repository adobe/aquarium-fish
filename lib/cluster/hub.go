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
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/adobe/aquarium-fish/lib/cluster/msg"
	"github.com/adobe/aquarium-fish/lib/log"
)

// Hub maintains the set of active clients and broadcasts messages to them.
type Hub struct {
	// Registered clients.
	clients map[*Client]bool

	// Message to be sent to all the clients.
	broadcast chan *msg.Message

	// Register requests from the clients.
	register chan *Client

	// Unregister requests from clients.
	unregister chan *Client
}

func newHub() *Hub {
	hub := &Hub{
		broadcast:  make(chan *msg.Message),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		clients:    make(map[*Client]bool),
	}

	go hub.run()

	return hub
}

func (h *Hub) Clients() (out []*Client) {
	for client := range h.clients {
		out = append(out, client)
	}
	return out
}

func (h *Hub) run() {
	for {
		select {
		case client := <-h.register:
			h.clients[client] = true
		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send_buf)
				log.Info("Cluster: Hub: connection closed")
			}
		case message := <-h.broadcast:
			data, err := json.Marshal(message)
			if err != nil {
				log.Error("Unable to marshal the message to broadcast:", err)
				continue
			}
			for client := range h.clients {
				if ok := client.processed_sums.Put(message.Sum); !ok {
					// The message was already processed or received by the client
					continue
				}
				select {
				case client.send_buf <- data:
				default:
					close(client.send_buf)
					delete(h.clients, client)
				}
			}
		}
	}
}

// Write data to broadcast
func (h *Hub) Broadcast(message *msg.Message) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*100)
	defer cancel()

	for {
		select {
		case h.broadcast <- message:
			return nil
		case <-ctx.Done():
			return fmt.Errorf("context canceled")
		}
	}
}
