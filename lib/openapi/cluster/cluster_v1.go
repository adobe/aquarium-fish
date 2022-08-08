/**
 * Copyright 2021 Adobe. All rights reserved.
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
	"crypto/x509"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"

	"github.com/adobe/aquarium-fish/lib/cluster"
	"github.com/adobe/aquarium-fish/lib/fish"
)

// H is a shortcut for map[string]interface{}
type H map[string]interface{}

type Processor struct {
	fish     *fish.Fish
	upgrader websocket.Upgrader

	hub *Hub
}

func NewV1Router(e *echo.Echo, fish *fish.Fish, cl *cluster.Cluster) {
	hub := &Hub{
		broadcast:  make(chan []byte),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		clients:    make(map[*Client]bool),
	}
	go hub.Run()
	proc := &Processor{
		fish: fish,
		upgrader: websocket.Upgrader{
			EnableCompression: true,
		},
		hub: hub,
	}
	router := e.Group("")
	router.Use(
		// The connected client should have valid cluster signed certificate
		proc.ClientCertAuth,
	)
	router.GET("/cluster/v1/connect", proc.ClusterConnect)
}

func (e *Processor) ClientCertAuth(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		// The connecting client should have the valid to cluster CA certificate, with the CN of
		// the node name, pubkey need be the same as stored (or first time registration) in cluster
		// nodes table and the time of last ping need to be more than ping delay time x2

		if len(c.Request().TLS.PeerCertificates) == 0 {
			return echo.NewHTTPError(http.StatusUnauthorized, "Client certificate is not provided")
		}

		var valid_client_cert *x509.Certificate
		for _, crt := range c.Request().TLS.PeerCertificates {
			// Validation over cluster CA cert
			opts := x509.VerifyOptions{
				Roots:     c.Echo().TLSServer.TLSConfig.ClientCAs,
				KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
			}
			_, err := crt.Verify(opts)
			if err != nil {
				log.Println(fmt.Sprintf("Cluster: Client %s (%s) certificate CA verify failed:",
					crt.Subject.CommonName, c.RealIP()), err)
				continue
			}

			// TODO: Check the node in db by CA as NodeName and if exists compare the pubkey
			log.Println("DEBUG: Client certificate CN: ", crt.Subject.CommonName)
			der, err := x509.MarshalPKIXPublicKey(crt.PublicKey)
			if err != nil {
				continue
			}
			log.Println("DEBUG: Client certificate pubkey der: ", der)

			valid_client_cert = crt
		}

		if valid_client_cert == nil {
			return echo.NewHTTPError(http.StatusUnauthorized, "Client certificate is invalid")
		}

		c.Set("client_cert", valid_client_cert)

		//res, err := e.fish.ResourceGetByIP(c.RealIP())
		//if err != nil {
		//	return echo.NewHTTPError(http.StatusUnauthorized, "Client IP was not found in the node Resources")
		//}

		return next(c)
	}
}

func (e *Processor) ClusterConnect(c echo.Context) error {
	ws, err := e.upgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Unable to connect with the cluster: %v", err)})
		return fmt.Errorf("Unable to connect with the cluster: %w", err)
	}
	client := &Client{hub: e.hub, conn: ws, send: make(chan []byte, 256)}
	e.hub.register <- client

	// Allow collection of memory referenced by the caller by doing all work in
	// new goroutines.
	go client.writePump()
	go client.readPump()

	return nil
}

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
	hub *Hub

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
				fmt.Printf("Cluster: Client %v readPump: reading error\n", c.conn.RemoteAddr())
			}
			break
		}
		message = bytes.TrimSpace(bytes.Replace(message, newline, space, -1))
		fmt.Printf("Cluster: Client %v readPump: got: %s\n", c.conn.RemoteAddr(), message)
		c.hub.broadcast <- message
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

// Hub maintains the set of active clients and broadcasts messages to the
// clients.
type Hub struct {
	// Registered clients.
	clients map[*Client]bool

	// Inbound messages from the clients.
	broadcast chan []byte

	// Register requests from the clients.
	register chan *Client

	// Unregister requests from clients.
	unregister chan *Client
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.clients[client] = true
		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
				fmt.Println("Cluster: Hub: connection closed")
			}
		case <-h.broadcast:
			for client := range h.clients {
				select {
				case client.send <- []byte("acknowledge"):
				default:
					close(client.send)
					delete(h.clients, client)
				}
			}
		}
	}
}
