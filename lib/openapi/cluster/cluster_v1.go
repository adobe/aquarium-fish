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
	"crypto/x509"
	"fmt"
	"net/http"

	"github.com/fasthttp/websocket"
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
	hub     *cluster.Hub
}

func NewV1Router(e *echo.Echo, fish *fish.Fish, cl *cluster.Cluster) {
	hub := cluster.NewHub()
	go hub.Run()

	proc := &Processor{
		fish: fish,
		upgrader: websocket.Upgrader{
			EnableCompression: true,
		},
		cluster: cl,
		hub:     hub,
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

		var valid_client_cert *x509.Certificate
		for _, crt := range c.Request().TLS.PeerCertificates {
			// Validation over cluster CA cert
			opts := x509.VerifyOptions{
				Roots:     c.Echo().TLSServer.TLSConfig.ClientCAs,
				KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
			}
			_, err := crt.Verify(opts)
			if err != nil {
				log.Warnf("Cluster: Client %s (%s) certificate CA verify failed: %v", crt.Subject.CommonName, c.RealIP(), err)
				continue
			}

			log.Debug("Cluster: Client certificate CN:", crt.Subject.CommonName)
			der, err := x509.MarshalPKIXPublicKey(crt.PublicKey)
			if err != nil {
				continue
			}
			log.Debug("Cluster: Client certificate pubkey der:", der)

			valid_client_cert = crt
		}

		if valid_client_cert == nil {
			return echo.NewHTTPError(http.StatusUnauthorized, "Client certificate is invalid")
		}

		// TODO: Check the node in db by CA as NodeName and if exists compare the pubkey

		c.Set("client_cert", valid_client_cert)

		return next(c)
	}
}

func (e *Processor) ClusterConnect(c echo.Context) error {
	ws, err := e.upgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Unable to connect with the cluster: %v", err)})
		return log.Errorf("Unable to connect with the cluster: %v", err)
	}

	cluster.NewClientReceiver(e.fish, e.cluster, e.hub, ws)

	return nil
}
