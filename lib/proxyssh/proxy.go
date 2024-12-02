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

package proxyssh

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/adobe/aquarium-fish/lib/fish"
	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/openapi/types"
)

// NOTE: This proxy was highly influenced by Remco Verhoef's ideas in
// https://github.com/dutchcoders/sshproxy, but have a little to no similarity with its ancestor.

// ProxyAccess keeps state of the SSH server
type ProxyAccess struct {
	fish         *fish.Fish
	serverConfig *ssh.ServerConfig

	// Listening address
	Address net.Addr

	// Keeps session info for auth, key is src address, value is Session
	sessions sync.Map
}

// Session is stored in ProxyAccess::sessions.
type Session struct {
	ResourceAccessor *types.ResourceAccess
	SrcAddr          net.Addr

	// This work group used to track the routines of the session
	// to make sure everything shutdown properly
	wg sync.WaitGroup
}

func (p *ProxyAccess) serveConnection(clientConn net.Conn) error {
	log.Infof("PROXYSSH: %s: Starting new session", clientConn.RemoteAddr())

	// Establish SSH connection
	srcConn, srcConnChannels, srcConnReqs, err := p.establishConnection(clientConn)
	if err != nil {
		return err
	}
	defer srcConn.Close()

	// Get session info from map
	session, err := p.getSession(srcConn.RemoteAddr().String())
	if err != nil {
		return err
	}

	// Getting the info about the destination resource
	resource, err := p.fish.ResourceGet(session.ResourceAccessor.ResourceUID)
	if err != nil {
		return log.Errorf("PROXYSSH: %s: Unable to retrieve Resource %s: %v", session.SrcAddr, session.ResourceAccessor.ResourceUID, err)
	}
	if resource.Authentication.Username == "" && resource.Authentication.Password == "" {
		return log.Errorf("PROXYSSH: %s: Resource Authentication not provided", session.SrcAddr)
	}

	// Establish destination connection
	dstConn, err := session.connectToDestination(resource)
	if err != nil {
		return err
	}
	defer dstConn.Close()

	// Start handling requests and channels concurrently
	session.wg.Add(1)
	go session.handleSourceRequests(srcConnReqs, dstConn)

	for newChannel := range srcConnChannels {
		session.wg.Add(1)
		go session.handleChannel(newChannel, dstConn)
	}

	// Wait for goroutines to finish
	session.wg.Wait()
	log.Infof("PROXYSSH: %s: Session closed", session.SrcAddr)
	return nil
}

func (p *ProxyAccess) establishConnection(clientConn net.Conn) (*ssh.ServerConn, <-chan ssh.NewChannel, <-chan *ssh.Request, error) {
	srcConn, srcConnChannels, srcConnReqs, err := ssh.NewServerConn(clientConn, p.serverConfig)
	if err != nil {
		return nil, nil, nil, log.Errorf("PROXYSSH: %s: Failed to establish server connection: %v", clientConn.RemoteAddr(), err)
	}
	return srcConn, srcConnChannels, srcConnReqs, nil
}

func (p *ProxyAccess) getSession(srcAddrString string) (*Session, error) {
	value, loaded := p.sessions.LoadAndDelete(srcAddrString)
	if !loaded || value == nil {
		return nil, fmt.Errorf("unable to load session record for %s", srcAddrString)
	}

	session, ok := value.(*Session)
	if !ok {
		return nil, fmt.Errorf("invalid type conversion while retrieving session for %s", srcAddrString)
	}
	return session, nil
}

func (s *Session) connectToDestination(res *types.Resource) (*ssh.Client, error) {
	dstAddr := net.JoinHostPort(res.IpAddr, "22")
	dstConfig := &ssh.ClientConfig{
		User: res.Authentication.Username,
		Auth: []ssh.AuthMethod{
			ssh.Password(res.Authentication.Password),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // #nosec G106 , remote always have new hostkey by design
	}
	dstConn, err := ssh.Dial("tcp", dstAddr, dstConfig)
	if err != nil {
		return nil, log.Errorf("PROXYSSH: %s: Unable to dial destination %q: %v", s.SrcAddr, dstAddr, err)
	}
	return dstConn, nil
}

func (s *Session) handleSourceRequests(srcConnReqs <-chan *ssh.Request, dstConn *ssh.Client) {
	defer s.wg.Done()
	log.Debugf("PROXYSSH: %s: Handling source requests", s.SrcAddr)

	for r := range srcConnReqs {
		s.handleRequest(r, dstConn)
	}
	log.Debugf("PROXYSSH: %s: Finished handling source requests", s.SrcAddr)
}

func (s *Session) handleChannel(ch ssh.NewChannel, dstConn ssh.Conn) {
	defer s.wg.Done()
	log.Debugf("PROXYSSH: %s: Handling new channel: %s", s.SrcAddr, ch.ChannelType())

	dstChn, dstChnRequests, dstChnErr := dstConn.OpenChannel(ch.ChannelType(), ch.ExtraData())
	if dstChnErr != nil {
		log.Errorf("PROXYSSH: %s: Could not open channel to destination: %v", s.SrcAddr, dstChnErr)
		ch.Reject(ssh.ConnectionFailed, "Unable to connect to destination resource")
		return
	}

	srcChn, srcChnRequests, srcChnErr := ch.Accept()
	if srcChnErr != nil {
		log.Errorf("PROXYSSH: %s: Could not accept source channel: %v", s.SrcAddr, srcChnErr)
		dstChn.Close()
		ch.Reject(ssh.ConnectionFailed, "Unable to accept connection")
		return
	}

	// Need this local channel work group to wait until all the channel routines completed
	var chWg sync.WaitGroup

	// Proxying the requests
	chWg.Add(1)
	go func() {
		defer chWg.Done()
		log.Debugf("PROXYSSH: %s: Starting to listen for channel requests", s.SrcAddr)
		for {
			var request *ssh.Request
			var targetChannel ssh.Channel

			select {
			case request = <-srcChnRequests:
				targetChannel = dstChn
			case request = <-dstChnRequests:
				targetChannel = srcChn
			}

			// In the event that an SSH request gets killed (not exited),
			// the request will be nil.  Do not continue, exit the loop
			if request == nil {
				log.Warnf("PROXYSSH: %s: SSH connection terminated ungracefully...", s.SrcAddr)
				break
			}

			requestValid, requestError := targetChannel.SendRequest(request.Type, request.WantReply, request.Payload)
			if requestError != nil {
				log.Errorf("PROXYSSH: %s: SendRequest error: %v", s.SrcAddr, requestError)
				break
			}

			if request.WantReply {
				if err := request.Reply(requestValid, nil); err != nil {
					log.Errorf("PROXYSSH: %s: Unable to respond to request %s: %v", s.SrcAddr, request.Type, err)
					break
				}
			}

			log.Debugf("PROXYSSH: %s: Request: Type=%q, WantReply='%t'.", s.SrcAddr, request.Type, request.WantReply)
			if request.Type == "exit-status" {
				// Ending the channel requests processing
				break
			}
		}

		// End the communication between the source and destination.
		srcChn.Close()
		dstChn.Close()

		log.Debugf("PROXYSSH: %s: Stopped to listen for the channel requests", s.SrcAddr)
	}()

	log.Debugf("PROXYSSH: %s: Begin streaming to and from %q.", s.SrcAddr, dstConn.RemoteAddr())

	chWg.Add(1)
	go func() {
		defer chWg.Done()
		log.Debugf("PROXYSSH: %s: Starting dst->src stream copy", s.SrcAddr)
		if _, err := io.Copy(dstChn, srcChn); err != nil && err != io.EOF {
			log.Errorf("PROXYSSH: %s: The dst->src channel was closed unexpectidly: %v", s.SrcAddr, err)
		}
		// Properly closing the channel
		dstChn.CloseWrite()
		srcChn.CloseWrite()
		log.Debugf("PROXYSSH: %s: The dst->src channel was closed", s.SrcAddr)
	}()

	log.Debugf("PROXYSSH: %s: Starting src->dst stream copy", s.SrcAddr)
	if _, err := io.Copy(srcChn, dstChn); err != nil && err != io.EOF {
		log.Errorf("PROXYSSH: %s: The src->dst channel was closed unexpectidly: %v", s.SrcAddr, err)
	}
	// Properly closing the channel
	dstChn.CloseWrite()
	srcChn.CloseWrite()
	log.Debugf("PROXYSSH: %s: The src->dst channel was closed", s.SrcAddr)

	chWg.Wait()
	log.Debugf("PROXYSSH: %s: Completed processing channel: %s", s.SrcAddr, ch.ChannelType())
}

func (s *Session) handleRequest(r *ssh.Request, c *ssh.Client) {
	log.Debugf("PROXYSSH: %s: Handling src request: %s", s.SrcAddr, r.Type)

	// Proxy to destination
	ok, data, err := c.SendRequest(r.Type, r.WantReply, r.Payload)
	if nil != err {
		log.Errorf("PROXYSSH: %s: Unable to proxy request %s: %v", s.SrcAddr, r.Type, err)
		return
	}

	// Pass to src
	if r.WantReply {
		if err := r.Reply(ok, data); nil != err {
			log.Errorf("PROXYSSH: %s: Unable to respond to request %s: %v", s.SrcAddr, r.Type, err)
			return
		}
	}
}

func (p *ProxyAccess) passwordCallback(incomingConn ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
	user := incomingConn.User()
	log.Debugf("PROXYSSH: %s: Login attempt for user %q.", incomingConn.RemoteAddr(), user)

	fishUser, err := p.fish.UserGet(user)
	if err != nil {
		log.Errorf("PROXYSSH: Unrecognized user %q", user)
		return nil, fmt.Errorf("Invalid access")
	}

	stringPass := string(pass)

	ra, err := p.fish.ResourceAccessSingleUsePassword(fishUser.Name, stringPass)
	if err != nil {
		log.Errorf("PROXYSSH: Invalid access for user %q: %v", user, err)
		return nil, fmt.Errorf("Invalid access")
	}

	// Only return non-error if the username and password match
	if ra.Username == user && ra.Password == stringPass {
		srcAddr := incomingConn.RemoteAddr()
		// If the session is not already stored in our map, create it so that
		// we have access to it when processing the incoming connections.
		p.sessions.LoadOrStore(srcAddr.String(), &Session{SrcAddr: srcAddr})
		return nil, nil
	}

	// Otherwise, we have failed, return error to indicate as such.
	return nil, fmt.Errorf("invalid access")
}

// Init starts SSH proxy
func Init(f *fish.Fish, idRsaPath string, address string) error {
	// First, try and read the file if it exists already. Otherwise, it is the
	// first execution, generate the private / public keys. The SSH server
	// requires at least one identity loaded to run.
	privateBytes, err := os.ReadFile(idRsaPath)
	if err != nil {
		// If it cannot be loaded, this is the first execution, generate it.
		log.Infof("PROXYSSH: Could not load %q, generating...", idRsaPath)
		rsaKey, err := rsa.GenerateKey(rand.Reader, 4096)
		if err != nil {
			return fmt.Errorf("PROXYSSH: Could not generate private key %q: %w", idRsaPath, err)
		}
		pemKey := pem.EncodeToMemory(
			&pem.Block{
				Type:  "RSA PRIVATE KEY",
				Bytes: x509.MarshalPKCS1PrivateKey(rsaKey),
			},
		)
		// Write out the new key file and load into `privateBytes` again.
		if err := os.WriteFile(idRsaPath, pemKey, 0600); err != nil {
			return fmt.Errorf("PROXYSSH: Could not write %q: %w", idRsaPath, err)
		}
		privateBytes, err = os.ReadFile(idRsaPath)
		if err != nil {
			return fmt.Errorf("PROXYSSH: Failed to load private key %q after generating: %w", idRsaPath, err)
		}
	}

	private, err := ssh.ParsePrivateKey(privateBytes)
	if err != nil {
		return fmt.Errorf("PROXYSSH: Failed to parse private key: %w", err)
	}

	server := ProxyAccess{fish: f}
	server.serverConfig = &ssh.ServerConfig{
		ServerVersion:    "SSH-2.0-AquriumFishProxy",
		PasswordCallback: server.passwordCallback,
	}
	server.serverConfig.AddHostKey(private)

	errChan := make(chan error)
	go func() {
		// Create the listener and let it wait for new connections in a separated goroutine
		listener, err := net.Listen("tcp", address)
		if err != nil {
			errChan <- err
			return
		}
		defer listener.Close()

		server.Address = listener.Addr()

		// Indefinitely accept new connections, process them concurrently
		for {
			incomingConn, err := listener.Accept() // Blocks until new connection comes
			if err != nil {
				log.Errorf("PROXYSSH: Unable to accept the incoming connection: %v", err)
				continue
			}

			go server.serveConnection(incomingConn)
		}
	}()

	// Wait for server start
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if server.Address != nil && strings.Contains(server.Address.String(), ":") {
				log.Info("PROXYSSH listening on:", server.Address)
				return nil // Was started
			}
		case err := <-errChan:
			return log.Errorf("PROXYSSH: Unable to bind to address %s: %v", address, err)
		}
	}
}
