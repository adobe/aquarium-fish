/**
 * Copyright 2024-2025 Adobe. All rights reserved.
 * This file is licensed to you under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License. You may obtain a copy
 * of the License at http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software distributed under
 * the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
 * OF ANY KIND, either express or implied. See the License for the specific language
 * governing permissions and limitations under the License.
 */

// Author: Sergei Parshev (@sparshev)

package proxyssh

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"sync"

	"golang.org/x/crypto/ssh"

	"github.com/adobe/aquarium-fish/lib/crypt"
	"github.com/adobe/aquarium-fish/lib/log"
	typesv2 "github.com/adobe/aquarium-fish/lib/types/aquarium/v2"
)

// NOTE: This proxy was highly influenced by Remco Verhoef's ideas in
// https://github.com/dutchcoders/sshproxy, but have a little to no similarity with its ancestor.

// session is stored in Driver::sessions.
type session struct {
	drv              *Driver
	ResourceAccessor *typesv2.GateProxySSHAccess
	SrcAddr          net.Addr

	// This work group used to track the routines of the session
	// to make sure everything shutdown properly
	wg sync.WaitGroup
}

func (d *Driver) serveConnection(clientConn net.Conn) error {
	logger := log.WithFunc("proxyssh", "serveConnection").With("gate.name", d.name, "client_addr", clientConn.RemoteAddr())
	logger.Info("Starting new session")

	// Establish SSH connection
	srcConn, srcConnChannels, srcConnReqs, err := d.establishConnection(clientConn)
	if err != nil {
		logger.Error("Failed to establish connection", "err", err)
		return fmt.Errorf("PROXYSSH: %s: %s: Failed to establish connection: %v", d.name, clientConn.RemoteAddr(), err)
	}
	defer srcConn.Close()
	logger.Debug("Established new connection", "src_session_id", srcConn.SessionID())

	// Get session info from map
	session, err := d.getSession(srcConn.SessionID())
	if err != nil {
		logger.Error("Failed to get session", "err", err)
		return fmt.Errorf("PROXYSSH: %s: %s: Failed to get session: %v", d.name, clientConn.RemoteAddr(), err)
	}

	if session.ResourceAccessor == nil {
		logger.Error("No ResourceAccessor is set for the session")
		return fmt.Errorf("PROXYSSH: %s: %s: No ResourceAccessor is set for the session", d.name, session.SrcAddr)
	}

	// Getting the info about the destination resource
	resource, err := d.db.ApplicationResourceGet(context.Background(), session.ResourceAccessor.ApplicationResourceUid)
	if err != nil {
		logger.Error("Unable to retrieve Resource", "appres_uid", session.ResourceAccessor.ApplicationResourceUid, "err", err)
		return fmt.Errorf("PROXYSSH: %s: %s: Unable to retrieve Resource %s: %v", d.name, session.SrcAddr, session.ResourceAccessor.ApplicationResourceUid, err)
	}
	if resource.Authentication == nil || resource.Authentication.Username == "" && resource.Authentication.Password == "" {
		logger.Error("Resource Authentication is not set")
		return fmt.Errorf("PROXYSSH: %s: %s: Resource Authentication not provided", d.name, session.SrcAddr)
	}

	// Establish destination connection
	dstConn, err := session.connectToDestination(resource)
	if err != nil {
		logger.Error("Unable to connect to destination", "err", err)
		return fmt.Errorf("PROXYSSH: %s: %s: Unable to connect to destination: %v", d.name, session.SrcAddr, err)
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
	logger.Info("Session closed")
	return nil
}

func (d *Driver) establishConnection(clientConn net.Conn) (*ssh.ServerConn, <-chan ssh.NewChannel, <-chan *ssh.Request, error) { //nolint:revive
	srcConn, srcConnChannels, srcConnReqs, err := ssh.NewServerConn(clientConn, d.serverConfig)
	if err != nil {
		log.WithFunc("proxyssh", "establishConnection").With("gate.name", d.name).Error("Failed to establish server connection", "err", err)
		return nil, nil, nil, fmt.Errorf("PROXYSSH: %s: %s: Failed to establish server connection: %v", d.name, clientConn.RemoteAddr(), err)
	}
	return srcConn, srcConnChannels, srcConnReqs, nil
}

func (d *Driver) getSession(sessionID []byte) (*session, error) {
	value, loaded := d.sessions.LoadAndDelete(string(sessionID))
	if !loaded || value == nil {
		return nil, fmt.Errorf("Unable to load session record for %s", sessionID)
	}

	session, ok := value.(*session)
	if !ok {
		return nil, fmt.Errorf("Invalid type conversion while retrieving session: %s", sessionID)
	}
	return session, nil
}

func (s *session) connectToDestination(res *typesv2.ApplicationResource) (*ssh.Client, error) {
	logger := log.WithFunc("proxyssh", "connectToDestination").With("gate.name", s.drv.name, "client_addr", s.SrcAddr)
	dstAddr := net.JoinHostPort(res.IpAddr, strconv.Itoa(int(res.Authentication.Port)))
	dstConfig := &ssh.ClientConfig{
		User:            res.Authentication.Username,
		Auth:            []ssh.AuthMethod{},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // #nosec G106 , remote always have new hostkey by design
	}

	// Use password auth if password is set for the Resource
	if res.Authentication.Password != "" {
		dstConfig.Auth = append(dstConfig.Auth, ssh.Password(res.Authentication.Password))
	}

	// Use private key if it's set for the Resource
	if res.Authentication.Key != "" {
		signer, err := ssh.ParsePrivateKey([]byte(res.Authentication.Key))
		if err != nil {
			logger.Error("Unable to parse private key len", "key_len", len(res.Authentication.Key), "err", err)
			return nil, fmt.Errorf("PROXYSSH: %s: %s: Unable to parse private key len %d: %v", s.drv.name, s.SrcAddr, len(res.Authentication.Key), err)
		}
		dstConfig.Auth = append(dstConfig.Auth, ssh.PublicKeys(signer))
	}

	dstConn, err := ssh.Dial("tcp", dstAddr, dstConfig)
	if err != nil {
		logger.Error("Unable to dial destination", "dst_addr", dstAddr, "err", err)
		return nil, fmt.Errorf("PROXYSSH: %s: %s: Unable to dial destination %q: %v", s.drv.name, s.SrcAddr, dstAddr, err)
	}
	return dstConn, nil
}

func (s *session) handleSourceRequests(srcConnReqs <-chan *ssh.Request, dstConn *ssh.Client) {
	defer s.wg.Done()
	logger := log.WithFunc("proxyssh", "handleSourceRequests").With("gate.name", s.drv.name, "client_addr", s.SrcAddr)
	logger.Debug("Handling source requests")
	defer logger.Debug("Finished handling source requests")

	for r := range srcConnReqs {
		s.handleRequest(r, dstConn)
	}
}

func (s *session) handleChannel(ch ssh.NewChannel, dstConn ssh.Conn) {
	defer s.wg.Done()
	logger := log.WithFunc("proxyssh", "handleChannel").With("gate.name", s.drv.name, "client_addr", s.SrcAddr, "dst_addr", dstConn.RemoteAddr())
	logger.Debug("Handling new channel", "channel_type", ch.ChannelType())

	// To prevent concurrent access to the channels
	var chnMutex sync.Mutex

	dstChn, dstChnRequests, dstChnErr := dstConn.OpenChannel(ch.ChannelType(), ch.ExtraData())
	if dstChnErr != nil {
		logger.Error("Could not open channel to destination", "err", dstChnErr)
		ch.Reject(ssh.ConnectionFailed, "Unable to connect to destination resource")
		return
	}

	srcChn, srcChnRequests, srcChnErr := ch.Accept()
	if srcChnErr != nil {
		logger.Error("Could not accept source channel", "err", srcChnErr)
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

		// End the communication between the source and destination when this function is complete.
		defer func() {
			chnMutex.Lock()
			srcChn.Close()
			dstChn.Close()
			chnMutex.Unlock()
		}()

		logger.Debug("Starting to listen for channel requests")
		defer logger.Debug("Stopped to listen for the channel requests")
		for {
			var request *ssh.Request
			var targetChannel ssh.Channel

			select {
			case request = <-srcChnRequests:
				//logger.Debug("Received src channel request", "request", request)
				targetChannel = dstChn
			case request = <-dstChnRequests:
				//logger.Debug("Received dst channel request", "request", request)
				targetChannel = srcChn
			}

			// In the event that an SSH request gets killed (not exited),
			// the request will be nil. Do not continue, exit the loop.
			if request == nil {
				logger.Warn("SSH connection terminated ungracefully...")
				break
			}

			requestValid, requestError := targetChannel.SendRequest(request.Type, request.WantReply, request.Payload)
			if requestError != nil {
				logger.Error("SendRequest error", "err", requestError)
				break
			}

			if request.WantReply {
				if err := request.Reply(requestValid, nil); err != nil {
					logger.Error("Unable to respond to request", "request_type", request.Type, "err", err)
					break
				}
			}

			logger.Debug("Request", "request_type", request.Type, "want_reply", request.WantReply)
			if request.Type == "exit-status" {
				// Ending the channel requests processing
				break
			}
		}
	}()

	logger.Debug("Begin streaming")

	chWg.Add(1)
	go func() {
		defer chWg.Done()
		logger.Debug("Starting dst->src stream copy")
		if _, err := io.Copy(srcChn, dstChn); err != nil && err != io.EOF {
			logger.Error("The dst->src channel was closed unexpectedly", "err", err)
		} else {
			logger.Debug("The dst->src channel was closed", "err", err)
		}
		chnMutex.Lock()
		defer chnMutex.Unlock()
		// Properly closing the channel
		if err := dstChn.CloseWrite(); err != nil {
			logger.Warn("The dst->src closing write for dst channel did not go well", "err", err)
		}
		if err := srcChn.CloseWrite(); err != nil {
			logger.Warn("The dst->src closing write for src channel did not go well", "err", err)
		}
	}()

	if _, err := io.Copy(dstChn, srcChn); err != nil && err != io.EOF {
		logger.Error("The src->dst channel was closed unexpectedly", "err", err)
	} else {
		logger.Debug("The src->dst channel was closed", "err", err)
	}
	// Properly closing the channel
	chnMutex.Lock()
	if err := dstChn.CloseWrite(); err != nil {
		logger.Warn("The src->dst closing write for dst channel did not go well", "err", err)
	}
	if err := srcChn.CloseWrite(); err != nil {
		logger.Warn("The src->dst closing write for src channel did not go well", "err", err)
	}
	chnMutex.Unlock()

	chWg.Wait()
	logger.Debug("Completed processing channel", "channel_type", ch.ChannelType())
}

func (s *session) handleRequest(r *ssh.Request, c *ssh.Client) {
	logger := log.WithFunc("proxyssh", "handleRequest").With("gate.name", s.drv.name, "client_addr", s.SrcAddr)
	logger.Debug("Handling src request", "request_type", r.Type)

	// Proxy to destination
	ok, data, err := c.SendRequest(r.Type, r.WantReply, r.Payload)
	if nil != err {
		logger.Error("Unable to proxy request", "request_type", r.Type, "err", err)
		return
	}

	// Pass to src
	if r.WantReply {
		if err := r.Reply(ok, data); nil != err {
			logger.Error("Unable to respond to request", "request_type", r.Type, "err", err)
			return
		}
	}
}

func (d *Driver) passwordCallback(incomingConn ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
	logger := log.WithFunc("proxyssh", "passwordCallback").With("gate.name", d.name, "client_addr", incomingConn.RemoteAddr())
	user := incomingConn.User()
	logger.Debug("Login attempt for user", "user", user)

	fishUser, err := d.db.UserGet(context.Background(), user)
	if err != nil {
		logger.Error("Unrecognized user", "user", user)
		return nil, fmt.Errorf("Invalid access")
	}

	// The proxy password is temporary (for the lifetime of the Resource) and one-time
	// so lack of salt will not be a big deal - the params will contribute to salt majorily.
	passHash := crypt.NewHash(string(pass), []byte{}).Hash
	passHashStr := fmt.Sprintf("%x", passHash)

	ra, err := d.db.GateProxySSHAccessSingleUsePasswordHash(fishUser.Name, passHashStr)
	if err != nil {
		logger.Error("Invalid access for user", "user", fishUser.Name, "err", err)
		return nil, fmt.Errorf("Invalid access")
	}

	// Only return non-error if the username and password match (double check just in case)
	if ra.Username == user && ra.Password == passHashStr {
		srcAddr := incomingConn.RemoteAddr()
		// If the session is not already stored in our map, create it so that
		// we have access to it when processing the incoming connections.
		d.sessions.LoadOrStore(string(incomingConn.SessionID()), &session{drv: d, SrcAddr: srcAddr, ResourceAccessor: ra})
		return nil, nil
	}

	// Otherwise, we have failed, return error to indicate as such.
	return nil, fmt.Errorf("Invalid access")
}

func (d *Driver) publicKeyCallback(incomingConn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
	logger := log.WithFunc("proxyssh", "publicKeyCallback").With("gate.name", d.name, "client_addr", incomingConn.RemoteAddr())
	user := incomingConn.User()
	logger.Debug("Login attempt for user", "user", user)

	fishUser, err := d.db.UserGet(context.Background(), user)
	if err != nil {
		logger.Error("Unrecognized user", "user", user)
		return nil, fmt.Errorf("Invalid access")
	}

	stringKey := string(ssh.MarshalAuthorizedKey(key))

	ra, err := d.db.GateProxySSHAccessSingleUseKey(fishUser.Name, stringKey)
	if err != nil {
		logger.Error("Invalid access for user", "user", fishUser.Name, "err", err)
		return nil, fmt.Errorf("Invalid access")
	}

	// Only return non-error if the username and key match (double check just in case)
	if ra.Username == user && ra.Key == stringKey {
		srcAddr := incomingConn.RemoteAddr()
		// If the session is not already stored in our map, create it so that
		// we have access to it when processing the incoming connections.
		d.sessions.LoadOrStore(string(incomingConn.SessionID()), &session{drv: d, SrcAddr: srcAddr, ResourceAccessor: ra})
		return nil, nil
	}

	// Otherwise, we have failed, return error to indicate as such.
	return nil, fmt.Errorf("Invalid access")
}

// Init starts SSH proxy and returns the actual listening address and error if happened
func (d *Driver) proxyInit(keyPath string) (string, error) {
	logger := log.WithFunc("proxyssh", "proxyInit").With("gate.name", d.name)
	// First, try and read the file if it exists already. Otherwise, it is the
	// first execution, generate the private / public keys. The SSH server
	// requires at least one identity loaded to run.
	privateBytes, err := os.ReadFile(keyPath)
	if err != nil {
		// If it cannot be loaded, this is the first execution, generate it.
		logger.Info("Could not load key, generating...", "key_path", keyPath)
		pemKey, err := crypt.GenerateSSHKey()
		if err != nil {
			return "", fmt.Errorf("PROXYSSH: %s: Could not generate private key: %w", d.name, err)
		}
		// Write out the new key file and load into `privateBytes` again.
		if err := os.WriteFile(keyPath, pemKey, 0600); err != nil {
			return "", fmt.Errorf("PROXYSSH: %s: Could not write %q: %w", d.name, keyPath, err)
		}
		privateBytes, err = os.ReadFile(keyPath)
		if err != nil {
			return "", fmt.Errorf("PROXYSSH: %s: Failed to load private key %q after generating: %w", d.name, keyPath, err)
		}
	}

	private, err := ssh.ParsePrivateKey(privateBytes)
	if err != nil {
		return "", fmt.Errorf("PROXYSSH: %s: Failed to parse private key: %w", d.name, err)
	}

	server := d
	server.serverConfig = &ssh.ServerConfig{
		ServerVersion:     "SSH-2.0-AquariumFishProxy",
		PasswordCallback:  server.passwordCallback,
		PublicKeyCallback: server.publicKeyCallback,
	}
	server.serverConfig.AddHostKey(private)

	// Create the listener and let it wait for new connections in a separated goroutine
	listener, err := net.Listen("tcp", d.cfg.BindAddress)
	if err != nil {
		logger.Error("Unable to bind to address", "bind_address", d.cfg.BindAddress, "err", err)
		return "", fmt.Errorf("PROXYSSH: %s: Unable to bind to address %q: %v", d.name, d.cfg.BindAddress, err)
	}

	go func() {
		logger.Debug("Start listening for the incoming connections")
		defer listener.Close()

		// Indefinitely accept new connections, process them concurrently
		for {
			incomingConn, err := listener.Accept() // Blocks until new connection comes
			if err != nil {
				logger.Error("Unable to accept the incoming connection", "err", err)
				continue
			}

			go server.serveConnection(incomingConn)
		}
	}()

	// WARN: Used by integration tests
	logger.Info("ProxySSH listening", "addr", listener.Addr())

	return listener.Addr().String(), nil
}
