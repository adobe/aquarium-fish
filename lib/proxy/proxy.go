package proxy

import (
	"log"
	"net"

	"github.com/armon/go-socks5"
	"golang.org/x/net/context"

	"git.corp.adobe.com/CI/aquarium-fish/lib/fish"
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
	log.Println("Proxy: Requested proxy from", req.RemoteAddr, "to", req.DestAddr)

	// Only the existing node resource can use the proxy
	res, err := p.fish.ResourceGetByIP(req.RemoteAddr.IP.String())
	if err != nil {
		log.Println("Proxy: Denied proxy from the unauthorized client", req.RemoteAddr)
		return ctx, false
	}

	// Make sure we have the address in the allow list and rewrite it
	dest := req.DestAddr.FQDN
	if dest == "" {
		dest = req.DestAddr.IP.String()
	}
	over_dest := p.fish.ResourceServiceMapping(res, dest)
	if over_dest == "" {
		log.Println("Proxy: Denied proxy from", req.RemoteAddr, "to", req.DestAddr)
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

	log.Println("Proxy: Allowed proxy from", req.RemoteAddr, "to", req.DestAddr)

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
