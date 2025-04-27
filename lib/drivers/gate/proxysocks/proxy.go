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

// Package proxysocks implements socks5 proxy that could be used by the Resource VM to reach outside world
package proxysocks

import (
	"net"

	"github.com/armon/go-socks5"
	"golang.org/x/net/context"

	"github.com/adobe/aquarium-fish/lib/database"
	"github.com/adobe/aquarium-fish/lib/log"
)

// resolverSkip is needed to skip the resolving
type resolverSkip struct{}

// Resolve function makes skip possible
func (resolverSkip) Resolve(ctx context.Context, _ /*name*/ string) (context.Context, net.IP, error) {
	// It's impossible to verify the access of the client
	// and determine the service mapping here so skipping this step
	return ctx, net.IP{}, nil
}

// proxyAccess configuration to store context while processing the proxy request
type proxyAccess struct {
	db *database.Database
}

// Allow will be executed to allow or deny proxy request
func (p *proxyAccess) Allow(ctx context.Context, req *socks5.Request) (context.Context, bool) {
	log.Debug("Proxy: Requested proxy from", req.RemoteAddr, "to", req.DestAddr)

	// Only the existing node resource can use the proxy
	res, err := p.db.ApplicationResourceGetByIP(req.RemoteAddr.IP.String())
	if err != nil {
		log.Warn("Proxy: Denied proxy from the unauthorized client:", req.RemoteAddr, err)
		return ctx, false
	}

	// Make sure we have the address in the allow list and rewrite it
	dest := req.DestAddr.FQDN
	if dest == "" {
		dest = req.DestAddr.IP.String()
	}
	overDest := p.db.ServiceMappingByApplicationAndDest(res.ApplicationUID, dest)
	if overDest == "" {
		log.Warn("Proxy: Denied proxy from", req.RemoteAddr, "to", req.DestAddr)
		return ctx, false
	}

	// Resolve destination address if it's not an IP
	req.DestAddr.IP = net.ParseIP(overDest)
	if req.DestAddr.IP == nil {
		req.DestAddr.FQDN = overDest
		addr, err := net.ResolveIPAddr("ip", req.DestAddr.FQDN)
		if err != nil {
			return ctx, false
		}
		req.DestAddr.IP = addr.IP
	}

	log.Debug("Proxy: Allowed proxy from", req.RemoteAddr, "to", req.DestAddr)

	return ctx, true
}

// Init will start the socks5 proxy server
func proxyInit(db *database.Database, address string) error {
	conf := &socks5.Config{
		Resolver: &resolverSkip{},  // Skipping the resolver phase until access checked
		Rules:    &proxyAccess{db}, // Allow only known resources to access proxy
	}

	server, err := socks5.New(conf)
	if err != nil {
		return err
	}

	go server.ListenAndServe("tcp", address)

	return nil
}
