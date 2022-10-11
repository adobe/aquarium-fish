package cluster

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Send pings to peer with this period
const ping_period = 30 * time.Second

type ClusterClient struct {
	url       url.URL
	send_buf  chan []byte
	ctx       context.Context
	ctxCancel context.CancelFunc

	mu     sync.RWMutex
	wsconn *websocket.Conn

	cluster *Cluster
}

func (conn *ClusterClient) Connect() *websocket.Conn {
	conn.mu.Lock()
	defer conn.mu.Unlock()
	if conn.wsconn != nil {
		return conn.wsconn
	}

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for ; ; <-ticker.C {
		select {
		case <-conn.ctx.Done():
			return nil
		default:
			config := &tls.Config{
				RootCAs:      conn.cluster.ca_pool,
				Certificates: []tls.Certificate{conn.cluster.certkey},
			}
			dialer := &websocket.Dialer{
				Proxy:             http.ProxyFromEnvironment,
				HandshakeTimeout:  45 * time.Second,
				TLSClientConfig:   config,
				EnableCompression: true,
			}
			ws, _, err := dialer.Dial(conn.url.String(), nil)
			if err != nil {
				conn.log(fmt.Sprintf("Cannot connect to websocket: %s", conn.url.String()), err)
				continue
			}

			conn.log("Connected to node", err)
			conn.wsconn = ws

			return conn.wsconn
		}
	}
}

func (conn *ClusterClient) listen() {
	conn.log("Listen for the messages", nil)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-conn.ctx.Done():
			return
		case <-ticker.C:
			for {
				ws := conn.Connect()
				if ws == nil {
					return
				}
				_, msg, err := ws.ReadMessage()
				if err != nil {
					conn.log("Cannot read websocket message", err)
					conn.closeWs()
					break
				}
				conn.log(fmt.Sprintf("Received msg: %x\n", msg), nil)
			}
		}
	}
}

// Write data to the websocket server
func (conn *ClusterClient) Write(payload interface{}) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*50)
	defer cancel()

	for {
		select {
		case conn.send_buf <- data:
			return nil
		case <-ctx.Done():
			return fmt.Errorf("context canceled")
		}
	}
}

func (conn *ClusterClient) listenWrite() {
	for data := range conn.send_buf {
		ws := conn.Connect()
		if ws == nil {
			conn.log("No websocket connection", fmt.Errorf("ws is nil"))
			continue
		}

		if err := ws.WriteMessage(
			websocket.TextMessage,
			data,
		); err != nil {
			conn.log("Write error", err)
		}
	}
}

// Close will send close message and shutdown websocket connection
func (conn *ClusterClient) Stop() {
	conn.ctxCancel()
	conn.closeWs()
}

// Close will send close message and shutdown websocket connection
func (conn *ClusterClient) closeWs() {
	conn.mu.Lock()
	if conn.wsconn != nil {
		conn.wsconn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		conn.wsconn.Close()
		conn.wsconn = nil
	}
	conn.mu.Unlock()
}

func (conn *ClusterClient) ping() {
	conn.log("Ping started", nil)
	ticker := time.NewTicker(ping_period)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			ws := conn.Connect()
			if ws == nil {
				continue
			}
			if err := conn.wsconn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(ping_period/2)); err != nil {
				conn.closeWs()
			}
		case <-conn.ctx.Done():
			return
		}
	}
}

func (conn *ClusterClient) log(msg string, err error) {
	if err != nil {
		log.Printf("ClusterClient %s: %s: %v\n", conn.url.Host, msg, err)
	} else {
		log.Printf("ClusterClient %s: %s\n", conn.url.Host, msg)
	}
}
