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
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/fasthttp/websocket"
	"github.com/google/uuid"

	"github.com/adobe/aquarium-fish/lib/cluster/msg"
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

// Client is used to communicate and receive connections to/from the Node
type Client struct {
	cluster *Cluster
	fish    *fish.Fish
	name    string // Eventually contains the node name we're connecting to or receiving connection from
	host    string // Contains host:port of the connection both for receiver and initiator

	// Used to store long-running operations wait groups
	long_ops map[string]*WaitGroupCount

	// Used when connecting to remote
	url url.URL

	// The websocket connection.
	mu sync.RWMutex
	ws *websocket.Conn

	ctx       context.Context
	ctxCancel context.CancelFunc

	// Buffered channel of outbound messages.
	send_buf chan []byte

	// Status of the client connection
	ConnFail error // Contains last error if connection to the remote node failed
	Stopped  bool  // In case the client was stopped and sitting there doing nothing

	// Optimization to skip sending duplicate messages
	// They are not stay here for long - just for ~2 minutes while cluster quickly syncs the data
	processed_sums *sumCache
}

// Receiving the incoming connection from remote node
func NewClientReceiver(fish *fish.Fish, cluster *Cluster, ws *websocket.Conn, name string) *Client {
	client := &Client{
		cluster:  cluster,
		fish:     fish,
		name:     name,
		host:     ws.RemoteAddr().String(),
		ws:       ws,
		long_ops: make(map[string]*WaitGroupCount),
		send_buf: make(chan []byte, 256),

		processed_sums: newSumCache(time.Minute*2, time.Second*30),
	}
	log.Debugf("Cluster: Client %q: Created new receiver client with host: %q", client.Ident(), client.Host())

	client.ctx, client.ctxCancel = context.WithCancel(context.Background())

	// Starting the new connected client processes
	go client.receiverWritePump()
	go client.receiverReadPump()

	// Registering the new client in hub
	cluster.hub.register <- client

	return client
}

// Initiates the connection to remote node
func NewClientInitiator(fish *fish.Fish, cluster *Cluster, addr url.URL) *Client {
	if cluster.GetInfo().UID != uuid.Nil {
		// Adding uid to the address as query param
		vals := addr.Query()
		vals.Set("uid", cluster.GetInfo().UID.String())
		addr.RawQuery = vals.Encode()
	}
	client := &Client{
		cluster:  cluster,
		fish:     fish,
		host:     addr.Host,
		url:      addr,
		long_ops: make(map[string]*WaitGroupCount),
		send_buf: make(chan []byte, 1),

		processed_sums: newSumCache(time.Minute*2, time.Second*30),
	}
	log.Debugf("Cluster: Client: Created new initiator client: %q", client.url.String())

	client.ctx, client.ctxCancel = context.WithCancel(context.Background())

	go client.initiatorListen()
	go client.initiatorListenWrite()
	go client.initiatorPing()

	// Registering the new client in hub
	cluster.hub.register <- client

	return client
}

func (c *Client) IsConnected() bool {
	return c.ws != nil
}

// Returns string that will somehow identify the client,
// if the name is not known it will return host and port
func (c *Client) Ident() string {
	if c.name == "" {
		return c.host
	}
	return c.name
}

// Returns name of the client
// It could be empty if handshake was not yet done
func (c *Client) Name() string {
	return c.name
}

func (c *Client) Host() string {
	return c.host
}

func (c *Client) Url() url.URL {
	return c.url
}

// Way to check when long-process is still executing
func (c *Client) IsLongOperationExecuting(name string) bool {
	_, ok := c.long_ops[name]
	return ok
}

// receiverReadPump pumps messages from the websocket connection to the processor
func (c *Client) receiverReadPump() {
	defer func() {
		c.cluster.GetHub().unregister <- c
		c.closeWs()
	}()
	//c.ws.SetReadLimit(maxMessageSize)
	c.ws.SetReadDeadline(time.Now().Add(pong_wait))
	c.ws.SetPongHandler(func(string) error { c.ws.SetReadDeadline(time.Now().Add(pong_wait)); return nil })
	for {
		_, data, err := c.ws.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Warnf("Cluster: Client %s: receiverReadPump: reading error", c.Ident())
			}
			break
		}
		data = bytes.TrimSpace(bytes.Replace(data, newline, space, -1))
		//log.Debugf("Cluster: Client %s: receiverReadPump: got: %s", c.Ident(), data)

		// Decode the message
		var message msg.Message

		// Seems json.Unmarshal doesn't really like to be executed in parallel so using decoder
		dec := json.NewDecoder(bytes.NewReader(data))

		// Reading multiple messages that could potentially be joined
		for {
			if err := dec.Decode(&message); err == io.EOF {
				break
			} else if err != nil {
				log.Warnf("Cluster: Client %s: Unable to unmarshal the message container: %v", c.Ident(), err)
				return
			}
			go c.processMessage(message)
		}
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
		c.closeWs()
	}()
	for {
		select {
		case <-c.ctx.Done():
			return
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
				log.Errorf("Cluster: Client %s: Cannot connect to websocket: %s: %v", c.Ident(), c.url.String(), err)
				c.ConnFail = err
				continue
			}

			// Receiving the server node name from certificate
			tls_conn, ok := ws.UnderlyingConn().(*tls.Conn)
			if !ok {
				c.ConnFail = log.Errorf("Cluster: Client %s: Non-TLS connection prohibited: %s", c.Ident(), c.url.String())
				continue
			}
			srv_certs := tls_conn.ConnectionState().PeerCertificates
			if len(srv_certs) < 1 {
				c.ConnFail = log.Errorf("Cluster: Client %s: Unable to find the server node certificates: %s", c.Ident(), c.url.String())
				continue
			}
			c.name = srv_certs[0].Subject.CommonName

			log.Infof("Cluster: Client %s: Connected to node: %s", c.Ident(), c.Host())
			c.ConnFail = nil
			c.ws = ws

			return c.ws
		}
	}
}

func (c *Client) initiatorListen() {
	log.Infof("Cluster: Client %s: Initiator: Listen for the messages", c.Ident())
	ticker := time.NewTicker(50 * time.Millisecond)
	defer func() {
		c.cluster.GetHub().unregister <- c
		ticker.Stop()
	}()
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
				_, data, err := ws.ReadMessage()
				if err != nil {
					log.Errorf("Cluster: Client %s: Initiator: Cannot read websocket message: %v", c.Ident(), err)
					c.closeWs()
					break
				}
				//log.Debugf("Cluster: Client %s: Initiator: Received msg: %s", c.Ident(), data)

				// Decode the message
				var message msg.Message

				// Seems json.Unmarshal doesn't really like to be executed in parallel so using decoder
				dec := json.NewDecoder(bytes.NewReader(data))

				// Reading multiple messages that could potentially be joined
				for {
					if err := dec.Decode(&message); err == io.EOF {
						break
					} else if err != nil {
						log.Warnf("Cluster: Client %s: Unable to unmarshal the message container: %v", c.Ident(), err)
						return
					}
					go c.processMessage(message)
				}
			}
		}
	}
}

// Function to write the requested data into the socket
func (c *Client) initiatorListenWrite() {
	for data := range c.send_buf {
		ws := c.Connect()
		if ws == nil {
			log.Errorf("Cluster: Client %s: Initiator: No websocket connection: ws is nil", c.Ident())
			continue
		}

		if err := ws.WriteMessage(
			websocket.TextMessage,
			data,
		); err != nil {
			log.Errorf("Cluster: Client %s: Write error: %v", c.Ident(), err)
		}
	}
}

// Close will send close message and shutdown websocket connection
func (c *Client) Stop() {
	c.ctxCancel()
	c.closeWs()
	c.Stopped = true
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
	log.Infof("Cluster: Client %s: Ping started", c.Ident())
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
