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
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"sync"

	"golang.org/x/crypto/ssh"

	"github.com/adobe/aquarium-fish/lib/crypt"
	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/openapi/types"
)

// NOTE: This proxy was highly influenced by Remco Verhoef's ideas in
// https://github.com/dutchcoders/sshproxy, but have a little to no similarity with its ancestor.

// session is stored in Driver::sessions.
type session struct {
	drv              *Driver
	ResourceAccessor *types.ApplicationResourceAccess
	SrcAddr          net.Addr

	// This work group used to track the routines of the session
	// to make sure everything shutdown properly
	wg sync.WaitGroup
}

func (d *Driver) serveConnection(clientConn net.Conn) error {
	log.Infof("PROXYSSH: %s: %s: Starting new session", d.name, clientConn.RemoteAddr())

	// Establish SSH connection
	srcConn, srcConnChannels, srcConnReqs, err := d.establishConnection(clientConn)
	if err != nil {
		return log.Errorf("PROXYSSH: %s: %s: Failed to establish connection: %v", d.name, clientConn.RemoteAddr(), err)
	}
	defer srcConn.Close()
	log.Debugf("PROXYSSH: %s: %s: Established new connection: %x", d.name, clientConn.RemoteAddr(), srcConn.SessionID())

	// Get session info from map
	session, err := d.getSession(srcConn.SessionID())
	if err != nil {
		return log.Errorf("PROXYSSH: %s: %s: Failed to get session: %v", d.name, clientConn.RemoteAddr(), err)
	}

	if session.ResourceAccessor == nil {
		return log.Errorf("PROXYSSH: %s: %s: No ResourceAccessor is set for the session", d.name, session.SrcAddr)
	}

	// Getting the info about the destination resource
	resource, err := d.db.ApplicationResourceGet(session.ResourceAccessor.ApplicationResourceUID)
	if err != nil {
		return log.Errorf("PROXYSSH: %s: %s: Unable to retrieve Resource %s: %v", d.name, session.SrcAddr, session.ResourceAccessor.ApplicationResourceUID, err)
	}
	if resource.Authentication == nil || resource.Authentication.Username == "" && resource.Authentication.Password == "" {
		return log.Errorf("PROXYSSH: %s: %s: Resource Authentication not provided", d.name, session.SrcAddr)
	}

	// Establish destination connection
	dstConn, err := session.connectToDestination(resource)
	if err != nil {
		return log.Errorf("PROXYSSH: %s: %s: Unable to connect to destination: %v", d.name, session.SrcAddr, err)
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
	log.Infof("PROXYSSH: %s: %s: Session closed", d.name, session.SrcAddr)
	return nil
}

func (d *Driver) establishConnection(clientConn net.Conn) (*ssh.ServerConn, <-chan ssh.NewChannel, <-chan *ssh.Request, error) { //nolint:revive
	srcConn, srcConnChannels, srcConnReqs, err := ssh.NewServerConn(clientConn, d.serverConfig)
	if err != nil {
		return nil, nil, nil, log.Errorf("PROXYSSH: %s: %s: Failed to establish server connection: %v", d.name, clientConn.RemoteAddr(), err)
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

func (s *session) connectToDestination(res *types.ApplicationResource) (*ssh.Client, error) {
	dstAddr := net.JoinHostPort(res.IpAddr, strconv.Itoa(res.Authentication.Port))
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
			return nil, log.Errorf("PROXYSSH: %s: %s: Unable to parse private key len %d: %v", s.drv.name, s.SrcAddr, len(res.Authentication.Key), err)
		}
		dstConfig.Auth = append(dstConfig.Auth, ssh.PublicKeys(signer))
	}

	dstConn, err := ssh.Dial("tcp", dstAddr, dstConfig)
	if err != nil {
		return nil, log.Errorf("PROXYSSH: %s: %s: Unable to dial destination %q: %v", s.drv.name, s.SrcAddr, dstAddr, err)
	}
	return dstConn, nil
}

func (s *session) handleSourceRequests(srcConnReqs <-chan *ssh.Request, dstConn *ssh.Client) {
	defer s.wg.Done()
	log.Debugf("PROXYSSH: %s: %s: Handling source requests", s.drv.name, s.SrcAddr)

	for r := range srcConnReqs {
		s.handleRequest(r, dstConn)
	}
	log.Debugf("PROXYSSH: %s: %s: Finished handling source requests", s.drv.name, s.SrcAddr)
}

func (s *session) handleChannel(ch ssh.NewChannel, dstConn ssh.Conn) {
	defer s.wg.Done()
	log.Debugf("PROXYSSH: %s: %s: Handling new channel: %s", s.drv.name, s.SrcAddr, ch.ChannelType())

	dstChn, dstChnRequests, dstChnErr := dstConn.OpenChannel(ch.ChannelType(), ch.ExtraData())
	if dstChnErr != nil {
		log.Errorf("PROXYSSH: %s: %s: Could not open channel to destination: %v", s.drv.name, s.SrcAddr, dstChnErr)
		ch.Reject(ssh.ConnectionFailed, "Unable to connect to destination resource")
		return
	}

	srcChn, srcChnRequests, srcChnErr := ch.Accept()
	if srcChnErr != nil {
		log.Errorf("PROXYSSH: %s: %s: Could not accept source channel: %v", s.drv.name, s.SrcAddr, srcChnErr)
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
		defer srcChn.Close()
		defer dstChn.Close()

		log.Debugf("PROXYSSH: %s: %s: Starting to listen for channel requests", s.drv.name, s.SrcAddr)
		for {
			var request *ssh.Request
			var targetChannel ssh.Channel

			select {
			case request = <-srcChnRequests:
				//log.Debugf("PROXYSSH: %s: %s: Received src channel request: %v", s.drv.name, s.SrcAddr, request)
				targetChannel = dstChn
			case request = <-dstChnRequests:
				//log.Debugf("PROXYSSH: %s: %s: Received dst channel request: %v", s.drv.name, s.SrcAddr, request)
				targetChannel = srcChn
			}

			// In the event that an SSH request gets killed (not exited),
			// the request will be nil. Do not continue, exit the loop.
			if request == nil {
				log.Warnf("PROXYSSH: %s: %s: SSH connection terminated ungracefully...", s.drv.name, s.SrcAddr)
				break
			}

			requestValid, requestError := targetChannel.SendRequest(request.Type, request.WantReply, request.Payload)
			if requestError != nil {
				log.Errorf("PROXYSSH: %s: %s: SendRequest error: %v", s.drv.name, s.SrcAddr, requestError)
				break
			}

			if request.WantReply {
				if err := request.Reply(requestValid, nil); err != nil {
					log.Errorf("PROXYSSH: %s: %s: Unable to respond to request %s: %v", s.drv.name, s.SrcAddr, request.Type, err)
					break
				}
			}

			log.Debugf("PROXYSSH: %s: %s: Request: Type=%q, WantReply='%t'.", s.drv.name, s.SrcAddr, request.Type, request.WantReply)
			if request.Type == "exit-status" {
				// Ending the channel requests processing
				break
			}
		}

		log.Debugf("PROXYSSH: %s: %s: Stopped to listen for the channel requests", s.drv.name, s.SrcAddr)
	}()

	log.Debugf("PROXYSSH: %s: %s: Begin streaming to and from %q.", s.drv.name, s.SrcAddr, dstConn.RemoteAddr())

	chWg.Add(1)
	go func() {
		defer chWg.Done()
		log.Debugf("PROXYSSH: %s: %s: Starting dst->src stream copy", s.drv.name, s.SrcAddr)
		if _, err := io.Copy(srcChn, dstChn); err != nil && err != io.EOF {
			log.Errorf("PROXYSSH: %s: %s: The dst->src channel was closed unexpectedly: %v", s.drv.name, s.SrcAddr, err)
		} else {
			log.Debugf("PROXYSSH: %s: %s: The dst->src channel was closed: %v", s.drv.name, s.SrcAddr, err)
		}
		// Properly closing the channel
		if err := dstChn.CloseWrite(); err != nil {
			log.Warnf("PROXYSSH: %s: %s: The dst->src closing write for dst channel did not go well: %v", s.drv.name, s.SrcAddr, err)
		}
		if err := srcChn.CloseWrite(); err != nil {
			log.Warnf("PROXYSSH: %s: %s: The dst->src closing write for src channel did not go well: %v", s.drv.name, s.SrcAddr, err)
		}
	}()

	if _, err := io.Copy(dstChn, srcChn); err != nil && err != io.EOF {
		log.Errorf("PROXYSSH: %s: %s: The src->dst channel was closed unexpectedly: %v", s.drv.name, s.SrcAddr, err)
	} else {
		log.Debugf("PROXYSSH: %s: %s: The src->dst channel was closed", s.drv.name, s.SrcAddr)
	}
	// Properly closing the channel
	if err := dstChn.CloseWrite(); err != nil {
		log.Warnf("PROXYSSH: %s: %s: The src->dst closing write for dst channel did not go well: %v", s.drv.name, s.SrcAddr, err)
	}
	if err := srcChn.CloseWrite(); err != nil {
		log.Warnf("PROXYSSH: %s: %s: The src->dst closing write for src channel did not go well: %v", s.drv.name, s.SrcAddr, err)
	}

	chWg.Wait()
	log.Debugf("PROXYSSH: %s: %s: Completed processing channel: %s", s.drv.name, s.SrcAddr, ch.ChannelType())
}

func (s *session) handleRequest(r *ssh.Request, c *ssh.Client) {
	log.Debugf("PROXYSSH: %s: %s: Handling src request: %s", s.drv.name, s.SrcAddr, r.Type)

	// Proxy to destination
	ok, data, err := c.SendRequest(r.Type, r.WantReply, r.Payload)
	if nil != err {
		log.Errorf("PROXYSSH: %s: %s: Unable to proxy request %s: %v", s.drv.name, s.SrcAddr, r.Type, err)
		return
	}

	// Pass to src
	if r.WantReply {
		if err := r.Reply(ok, data); nil != err {
			log.Errorf("PROXYSSH: %s: %s: Unable to respond to request %s: %v", s.drv.name, s.SrcAddr, r.Type, err)
			return
		}
	}
}

func (d *Driver) passwordCallback(incomingConn ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
	user := incomingConn.User()
	log.Debugf("PROXYSSH: %s: %s: Login attempt for user %q.", d.name, incomingConn.RemoteAddr(), user)

	fishUser, err := d.db.UserGet(user)
	if err != nil {
		log.Errorf("PROXYSSH: %s: %s: Unrecognized user %q", d.name, incomingConn.RemoteAddr(), user)
		return nil, fmt.Errorf("Invalid access")
	}

	// The proxy password is temporary (for the lifetime of the Resource) and one-time
	// so lack of salt will not be a big deal - the params will contribute to salt majorily.
	passHash := crypt.NewHash(string(pass), []byte{}).Hash
	passHashStr := fmt.Sprintf("%x", passHash)

	ra, err := d.db.ApplicationResourceAccessSingleUsePasswordHash(fishUser.Name, passHashStr)
	if err != nil {
		log.Errorf("PROXYSSH: %s: %s: Invalid access for user %q: %v", d.name, incomingConn.RemoteAddr(), fishUser.Name, err)
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
	user := incomingConn.User()
	log.Debugf("PROXYSSH: %s: %s: Login attempt for user %q.", d.name, incomingConn.RemoteAddr(), user)

	fishUser, err := d.db.UserGet(user)
	if err != nil {
		log.Errorf("PROXYSSH: %s: %s: Unrecognized user %q", d.name, incomingConn.RemoteAddr(), user)
		return nil, fmt.Errorf("Invalid access")
	}

	stringKey := string(ssh.MarshalAuthorizedKey(key))

	ra, err := d.db.ApplicationResourceAccessSingleUseKey(fishUser.Name, stringKey)
	if err != nil {
		log.Errorf("PROXYSSH: %s: %s: Invalid access for user %q: %v", d.name, incomingConn.RemoteAddr(), fishUser.Name, err)
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
	// First, try and read the file if it exists already. Otherwise, it is the
	// first execution, generate the private / public keys. The SSH server
	// requires at least one identity loaded to run.
	privateBytes, err := os.ReadFile(keyPath)
	if err != nil {
		// If it cannot be loaded, this is the first execution, generate it.
		log.Infof("PROXYSSH: %s: Could not load %q, generating...", d.name, keyPath)
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
		return "", log.Errorf("PROXYSSH: %s: Unable to bind to address %q: %v", d.name, d.cfg.BindAddress, err)
	}

	go func() {
		log.Debugf("PROXYSSH: %s: Start listening for the incoming connections", d.name)
		defer listener.Close()

		// Indefinitely accept new connections, process them concurrently
		for {
			incomingConn, err := listener.Accept() // Blocks until new connection comes
			if err != nil {
				log.Errorf("PROXYSSH: %s: Unable to accept the incoming connection: %v", d.name, err)
				continue
			}

			go server.serveConnection(incomingConn)
		}
	}()

	log.Infof("PROXYSSH listening on: %s", d.name, listener.Addr())

	return listener.Addr().String(), nil
}
