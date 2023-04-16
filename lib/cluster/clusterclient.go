package cluster

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/adobe/aquarium-fish/lib/log"
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
				log.Errorf("ClusterClient %s: Cannot connect to websocket: %s: %v", conn.url.Host, conn.url.String(), err)
				continue
			}

			log.Infof("ClusterClient %s: Connected to node", conn.url.Host)
			conn.wsconn = ws

			return conn.wsconn
		}
	}
}

func (conn *ClusterClient) listen() {
	log.Infof("ClusterClient %s: Listen for the messages", conn.url.Host)
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
				_, _ /*msg*/, err := ws.ReadMessage()
				if err != nil {
					log.Errorf("ClusterClient %s: Cannot read websocket message: %v", conn.url.Host, err)
					conn.closeWs()
					break
				}
				//log.Printf("ClusterClient %s: Received msg: %x\n", conn.url.Host, msg)
				// TODO: Process msg
			}
		}
	}
}

// Write data to the websocket server
func (conn *ClusterClient) Write(payload any) error {
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
			log.Errorf("ClusterClient %s: No websocket connection: %v", conn.url.Host, fmt.Errorf("ws is nil"))
			continue
		}

		if err := ws.WriteMessage(
			websocket.TextMessage,
			data,
		); err != nil {
			log.Errorf("ClusterClient %s: Write error: %v", conn.url.Host, err)
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
	log.Infof("ClusterClient %s: Ping started", conn.url.Host)
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
