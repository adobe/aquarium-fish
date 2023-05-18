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
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/url"
	"os"

	"github.com/adobe/aquarium-fish/lib/fish"
)

type Cluster struct {
	fish *fish.Fish

	clients []*ClusterClient

	ca_pool *x509.CertPool
	certkey tls.Certificate
}

func New(fish *fish.Fish, join []string, ca_path, cert_path, key_path string) (*Cluster, error) {
	c := &Cluster{
		fish:    fish,
		ca_pool: x509.NewCertPool(),
	}

	// Load CA cert to pool
	ca_bytes, err := os.ReadFile(ca_path)
	if err != nil {
		return nil, fmt.Errorf("Cluster: Unable to load CA certificate: %v", err)
	}
	if !c.ca_pool.AppendCertsFromPEM(ca_bytes) {
		return nil, fmt.Errorf("Cluster: Incorrect CA pem data: %s", ca_path)
	}

	// Load client cert and key
	c.certkey, err = tls.LoadX509KeyPair(cert_path, key_path)
	if err != nil {
		return nil, fmt.Errorf("Cluster: Unable to load cert/key: %v", err)
	}

	// Connect the join nodes
	for _, endpoint := range join {
		c.NewClient(endpoint, "cluster/v1/connect")
	}

	return c, nil
}

func (c *Cluster) NewClient(host, channel string) *ClusterClient {
	conn := &ClusterClient{
		fish:     c.fish,
		url:      url.URL{Scheme: "wss", Host: host, Path: channel},
		send_buf: make(chan []byte, 1),
		cluster:  c,
	}
	conn.ctx, conn.ctxCancel = context.WithCancel(context.Background())

	go conn.listen()
	go conn.listenWrite()
	go conn.ping()

	c.clients = append(c.clients, conn)

	return conn
}

func (c *Cluster) Stop() {
	for _, conn := range c.clients {
		conn.Stop()
	}
}
