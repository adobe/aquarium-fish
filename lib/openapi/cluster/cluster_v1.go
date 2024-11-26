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
	"crypto/x509"
	"fmt"
	"net/http"

	"github.com/fasthttp/websocket"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/adobe/aquarium-fish/lib/cluster"
	"github.com/adobe/aquarium-fish/lib/fish"
	"github.com/adobe/aquarium-fish/lib/log"
)

// H is a shortcut for map[string]any
type H map[string]any

type Processor struct {
	fish     *fish.Fish
	upgrader websocket.Upgrader

	cluster *cluster.Cluster
}

func NewV1Router(e *echo.Echo, fish *fish.Fish, cl *cluster.Cluster) {
	proc := &Processor{
		fish: fish,
		upgrader: websocket.Upgrader{
			EnableCompression: true,
		},
		cluster: cl,
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
		// nodes table

		if len(c.Request().TLS.PeerCertificates) == 0 {
			return echo.NewHTTPError(http.StatusUnauthorized, "Client certificate is not provided")
		}

		// Finding the first valid client certificate
		var valid_client_cert *x509.Certificate
		for _, crt := range c.Request().TLS.PeerCertificates {
			// Validation over cluster CA cert
			opts := x509.VerifyOptions{
				Roots:     c.Echo().TLSServer.TLSConfig.ClientCAs,
				KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
			}
			_, err := crt.Verify(opts)
			if err != nil {
				log.Warnf("Cluster: Client %q (%s): certificate CA verify failed: %v", crt.Subject.CommonName, c.RealIP(), err)
				continue
			}

			client_der, err := x509.MarshalPKIXPublicKey(crt.PublicKey)
			if err != nil {
				log.Warnf("Cluster: Client %q (%s): Unable to marshal public key: %x", crt.Subject.CommonName, c.RealIP(), err)
				continue
			}
			log.Debugf("Cluster: Client %q (%s): certificate pubkey der: %x", crt.Subject.CommonName, c.RealIP(), client_der)

			// Checking existing Node with cert CA name with non-nil pubkey data
			if node, err := e.fish.NodeGet(crt.Subject.CommonName); err == nil {
				// The Node with this name is available in the DB, so checking it's pubkey
				if node.Pubkey != nil && bytes.Equal(*node.Pubkey, client_der) == false {
					log.Warnf("Cluster: Client %q (%s): certificate pubkey did not matched the known one: %x != %x", crt.Subject.CommonName, c.RealIP(), client_der, *node.Pubkey)
					continue
				}
			}

			// Checking there is no nodes with the same pubkey (to eliminate the copy of crt to be used)
			if nodes, err := e.fish.NodeGetPubkey(client_der); err == nil && len(nodes) > 0 {
				// Seems we have some known nodes with the same pubkey
				found_dup := false
				for _, node := range nodes {
					if node.Name != crt.Subject.CommonName {
						log.Warnf("Cluster: Client %q (%s): certificate pubkey reused by another node: %q", crt.Subject.CommonName, c.RealIP(), node.Name)
						found_dup = true
					}
				}
				if found_dup {
					continue
				}
			}

			valid_client_cert = crt
			break
		}

		if valid_client_cert == nil {
			log.Warnf("Cluster: Client (%s): Failed to validate all the provided certificates", c.RealIP())
			return echo.NewHTTPError(http.StatusUnauthorized, "Client certificates are invalid")
		}

		c.Set("client_cert", valid_client_cert)
		c.Set("client_name", valid_client_cert.Subject.CommonName)

		return next(c)
	}
}

func (e *Processor) ClusterConnect(c echo.Context) error {
	// Check cluster UID
	cluster_uid, err := uuid.Parse(c.QueryParam("uid"))
	if err == nil && e.cluster.GetInfo().UID != cluster_uid {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Incorrect connect cluster UID: %q", cluster_uid)})
		return log.Errorf("Incorrect connect cluster UID: %q != %q", e.cluster.GetInfo().UID, cluster_uid)
	}

	ws, err := e.upgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Unable to connect with the cluster: %v", err)})
		return log.Errorf("Unable to connect with the cluster: %v", err)
	}

	name := c.Get("client_name")
	if name.(string) == "" {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Node name from certificate can't be empty to connect to the cluster")})
		return fmt.Errorf("Node name from certificate can't be empty to connect to the cluster")
	}

	cluster.NewClientReceiver(e.fish, e.cluster, ws, name.(string))

	return nil
}
