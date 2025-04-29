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

// Allow will be executed to allow or deny proxy request
func (d *Driver) Allow(ctx context.Context, req *socks5.Request) (context.Context, bool) {
	log.Debugf("PROXYSOCKS: %s: Requested proxy from %s to %s", d.name, req.RemoteAddr, req.DestAddr)

	// Only the existing node resource can use the proxy
	res, err := d.db.ApplicationResourceGetByIP(req.RemoteAddr.IP.String())
	if err != nil {
		log.Warnf("PROXYSOCKS: %s: Denied proxy from the unauthorized client %s: %v", d.name, req.RemoteAddr, err)
		return ctx, false
	}

	// Make sure we have the address in the allow list and rewrite it
	dest := req.DestAddr.FQDN
	if dest == "" {
		dest = req.DestAddr.IP.String()
	}
	overDest := d.db.ServiceMappingByApplicationAndDest(res.ApplicationUID, dest)
	if overDest == "" {
		log.Warnf("PROXYSOCKS: %s: Denied proxy from %s to %s", d.name, req.RemoteAddr, req.DestAddr)
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

	log.Debugf("PROXYSOCKS: %s: Allowed proxy from %s to %s", d.name, req.RemoteAddr, req.DestAddr)

	return ctx, true
}

// Init will start the socks5 proxy server
func (d *Driver) proxyInit() error {
	conf := &socks5.Config{
		Resolver: &resolverSkip{}, // Skipping the resolver phase until access checked
		Rules:    d,               // Allow only known resources to access proxy
	}

	server, err := socks5.New(conf)
	if err != nil {
		return err
	}

	go server.ListenAndServe("tcp", d.cfg.BindAddress)

	return nil
}
