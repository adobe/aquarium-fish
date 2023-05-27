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
	"github.com/adobe/aquarium-fish/lib/log"
)

// Hub maintains the set of active clients and broadcasts messages to them.
type Hub struct {
	// Registered clients.
	clients map[*Client]bool

	// Message to be sent to all the clients.
	broadcast chan []byte

	// Register requests from the clients.
	register chan *Client

	// Unregister requests from clients.
	unregister chan *Client
}

func NewHub() *Hub {
	return &Hub{
		broadcast:  make(chan []byte),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		clients:    make(map[*Client]bool),
	}
}

func (h *Hub) Run() {
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
		case msg := <-h.broadcast:
			for client := range h.clients {
				select {
				case client.send_buf <- msg:
				default:
					close(client.send_buf)
					delete(h.clients, client)
				}
			}
		}
	}
}
