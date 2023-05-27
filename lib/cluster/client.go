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

	"github.com/adobe/aquarium-fish/lib/fish"
	"github.com/adobe/aquarium-fish/lib/log"
)

const (
	// Time allowed to write a message to the peer.
	write_wait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pong_wait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pong_wait.
	ping_period = (pong_wait * 9) / 10

	// Maximum message size allowed from peer.
	//maxMessageSize = 512
)

var (
	newline = []byte{'\n'}
	space   = []byte{' '}
)

// Client is a middleman between the websocket connection and the hub.
type Client struct {
	cluster *Cluster
	fish    *fish.Fish
	hub     *Hub
	ident   string // Contains identifier of the connection both for receiver and initiator

	// Used when connecting to remote
	url url.URL

	ctx       context.Context
	ctxCancel context.CancelFunc

	// The websocket connection.
	mu sync.RWMutex
	ws *websocket.Conn

	// Buffered channel of outbound messages.
	send_buf chan []byte

	// Status of the client connection
	ConnFail error // Contains last error if connection to the remote node failed
	Valid    bool  // Remote cluster is good to use
	InSync   bool  // True when the client successfully synchronized
}

// Receiving the incoming connection from remote node
func NewClientReceiver(fish *fish.Fish, cluster *Cluster, hub *Hub, ws *websocket.Conn) *Client {
	client := &Client{
		cluster:  cluster,
		fish:     fish,
		hub:      hub,
		ident:    ws.RemoteAddr().String(),
		ws:       ws,
		send_buf: make(chan []byte, 256),
	}

	hub.register <- client

	// Starting the new connected client processes
	go client.receiverWritePump()
	go client.receiverReadPump()

	return client
}

// Initiates the connection to remote node
func NewClientInitiator(fish *fish.Fish, cluster *Cluster, addr url.URL) *Client {
	cl := &Client{
		cluster:  cluster,
		fish:     fish,
		ident:    addr.Host,
		url:      addr,
		send_buf: make(chan []byte, 1),
	}
	cl.ctx, cl.ctxCancel = context.WithCancel(context.Background())

	go cl.initiatorListen()
	go cl.initiatorListenWrite()
	go cl.initiatorPing()

	return cl
}

func (c *Client) IsConnected() bool {
	return c.ws != nil
}

// receiverReadPump pumps messages from the websocket connection to the processor
func (c *Client) receiverReadPump() {
	defer func() {
		c.hub.unregister <- c
		c.ws.Close()
	}()
	//c.ws.SetReadLimit(maxMessageSize)
	c.ws.SetReadDeadline(time.Now().Add(pong_wait))
	c.ws.SetPongHandler(func(string) error { c.ws.SetReadDeadline(time.Now().Add(pong_wait)); return nil })
	for {
		_, message, err := c.ws.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Warnf("Cluster: Client %s: receiverReadPump: reading error", c.ident)
			}
			break
		}
		message = bytes.TrimSpace(bytes.Replace(message, newline, space, -1))
		//log.Debugf("Cluster: Client %s: receiverReadPump: got: %s", c.ident, message)
		go c.processMessage(message)
	}
}

// Write data to websocket
func (c *Client) Write(payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*100)
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

// receiverWritePump pumps messages to the websocket connection.
func (c *Client) receiverWritePump() {
	ticker := time.NewTicker(ping_period)
	defer func() {
		ticker.Stop()
		c.ws.Close()
	}()
	for {
		select {
		case message, ok := <-c.send_buf:
			c.ws.SetWriteDeadline(time.Now().Add(write_wait))
			if !ok {
				// The hub closed the channel.
				c.ws.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.ws.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			// Add queued chat messages to the current websocket message.
			n := len(c.send_buf)
			for i := 0; i < n; i++ {
				w.Write(newline)
				w.Write(<-c.send_buf)
			}

			if err := w.Close(); err != nil {
				return
			}
		case <-ticker.C:
			c.ws.SetWriteDeadline(time.Now().Add(write_wait))
			if err := c.ws.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (c *Client) Connect() *websocket.Conn {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.IsConnected() {
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
				log.Errorf("Cluster: Client %s: Cannot connect to websocket: %s: %v", c.ident, c.url.String(), err)
				c.ConnFail = err
				continue
			}

			log.Infof("Cluster: Client %s: Connected to node", c.ident)
			c.ConnFail = nil
			c.ws = ws

			// The node is connected to the cluster - so starting the sync process to ensure the
			// node is up to date (TODO: before interacting with the cluster)
			go c.syncRequest()

			return c.ws
		}
	}
}

func (c *Client) initiatorListen() {
	log.Infof("Cluster: Client %s: Initiator: Listen for the messages", c.ident)
	ticker := time.NewTicker(50 * time.Millisecond)
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
					log.Errorf("Cluster: Client %s: Initiator: Cannot read websocket message: %v", c.ident, err)
					c.closeWs()
					break
				}
				//log.Debugf("Cluster: Client %s: Initiator: Received msg: %s", c.ident, message)
				go c.processMessage(message)
			}
		}
	}
}

// Function to write the requested data into the socket
func (c *Client) initiatorListenWrite() {
	for data := range c.send_buf {
		ws := c.Connect()
		if ws == nil {
			log.Errorf("Cluster: Client %s: Initiator: No websocket connection: ws is nil", c.ident)
			continue
		}

		if err := ws.WriteMessage(
			websocket.TextMessage,
			data,
		); err != nil {
			log.Errorf("Cluster: Client %s: Write error: %v", c.ident, err)
		}
	}
}

// Close will send close message and shutdown websocket connection
func (c *Client) Stop() {
	c.ctxCancel()
	c.closeWs()
}

// Close will send close message and shutdown websocket connection
func (c *Client) closeWs() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ws != nil {
		c.ws.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		c.ws.Close()
		c.ws = nil
	}
}

// Ensures the socket is active and not silently dropped
func (c *Client) initiatorPing() {
	log.Infof("Cluster: Client %s: Ping started", c.ident)
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
