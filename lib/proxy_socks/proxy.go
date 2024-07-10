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

package proxy_socks

import (
	"net"

	"github.com/armon/go-socks5"
	"golang.org/x/net/context"

	"github.com/adobe/aquarium-fish/lib/fish"
	"github.com/adobe/aquarium-fish/lib/log"
)

type ResolverSkip struct{}

func (d ResolverSkip) Resolve(ctx context.Context, name string) (context.Context, net.IP, error) {
	// It's impossible to verify the access of the client
	// and determine the service mapping here so skipping this step
	return ctx, net.IP{}, nil
}

type ProxyAccess struct {
	fish *fish.Fish
}

func (p *ProxyAccess) Allow(ctx context.Context, req *socks5.Request) (context.Context, bool) {
	log.Debug("Proxy: Requested proxy from", req.RemoteAddr, "to", req.DestAddr)

	// Only the existing node resource can use the proxy
	res, err := p.fish.ResourceGetByIP(req.RemoteAddr.IP.String())
	if err != nil {
		log.Warn("Proxy: Denied proxy from the unauthorized client:", req.RemoteAddr, err)
		return ctx, false
	}

	// Make sure we have the address in the allow list and rewrite it
	dest := req.DestAddr.FQDN
	if dest == "" {
		dest = req.DestAddr.IP.String()
	}
	over_dest := p.fish.ResourceServiceMapping(res, dest)
	if over_dest == "" {
		log.Warn("Proxy: Denied proxy from", req.RemoteAddr, "to", req.DestAddr)
		return ctx, false
	}

	// Resolve destination address if it's not an IP
	req.DestAddr.IP = net.ParseIP(over_dest)
	if req.DestAddr.IP == nil {
		req.DestAddr.FQDN = over_dest
		addr, err := net.ResolveIPAddr("ip", req.DestAddr.FQDN)
		if err != nil {
			return ctx, false
		}
		req.DestAddr.IP = addr.IP
	}

	log.Debug("Proxy: Allowed proxy from", req.RemoteAddr, "to", req.DestAddr)

	return ctx, true
}

func Init(fish *fish.Fish, address string) error {
	conf := &socks5.Config{
		Resolver: &ResolverSkip{},    // Skipping the resolver phase until access checked
		Rules:    &ProxyAccess{fish}, // Allow only known resources to access proxy
	}

	server, err := socks5.New(conf)
	if err != nil {
		return err
	}

	go server.ListenAndServe("tcp", address)

	return nil
}
