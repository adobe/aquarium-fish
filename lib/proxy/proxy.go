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
	f    *fish.Fish
	orig socks5.PermitCommand
}

func (p *ProxyAccess) Allow(ctx context.Context, req *socks5.Request) (context.Context, bool) {
	log.Println("Proxy: Requested access from", req.RemoteAddr, "to", req.DestAddr)

	// TODO: Verify access of the client
	ok := true

	if req.DestAddr.FQDN != "" {
		// TODO: Override the address via the mapping
		addr, err := net.ResolveIPAddr("ip", req.DestAddr.FQDN)
		if err != nil {
			return ctx, false
		}
		req.DestAddr.IP = addr.IP
	}
	log.Println("Proxy: Allowed access from", req.RemoteAddr, "to", req.DestAddr)

	return ctx, ok
}

func Init(fish *fish.Fish, address string) error {
	conf := &socks5.Config{
		Resolver: &ResolverSkip{},                            // Skipping the resolver phase until access checked
		Rules:    &ProxyAccess{fish, socks5.PermitCommand{}}, // Allow only known resources to access proxy
	}

	server, err := socks5.New(conf)
	if err != nil {
		return err
	}

	go server.ListenAndServe("tcp", address)

	return nil
}
