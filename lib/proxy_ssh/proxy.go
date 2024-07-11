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
/**
 * This file has been modified from: https://github.com/dutchcoders/sshproxy
 *
 * The MIT License (MIT)
 * Copyright (c) 2015 Remco Verhoef
 * Permission is hereby granted, free of charge, to any person obtaining a copy
 * of this software and associated documentation files (the "Software"), to deal
 * in the Software without restriction, including without limitation the rights
 * to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
 * copies of the Software, and to permit persons to whom the Software is
 * furnished to do so, subject to the following conditions:
 *
 * The above copyright notice and this permission notice shall be included in all
 * copies or substantial portions of the Software.
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
 * IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
 * FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
 * AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
 * LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
 * OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
 * SOFTWARE.
 */

package proxy_ssh

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"os"
	"sync"

	"golang.org/x/crypto/ssh"

	"github.com/adobe/aquarium-fish/lib/fish"
	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/openapi/types"
)

// NOTE: this init function and logging information exist to satisfy binary
// redistribution of the code in this file.  Do not remove this.
func init() {
	log.Info("The Fish SSH proxy is a re-implementation of Remco Verhoef's MIT licensed example (https://github.com/dutchcoders/sshproxy)")
}

type ProxyAccess struct {
	fish         *fish.Fish
	serverConfig *ssh.ServerConfig
	// Keys: remote address from ssh connection
	// Values: tuple (username, password)
	// [all strings]
	sessions sync.Map
}

// Stored in ProxyAccess::sessions.
type SessionRecord struct {
	ResourceAccessor *types.ResourceAccess
	RemoteAddr       net.Addr
	// Username   string
	// Password   string
}

func (p *ProxyAccess) serveConnection(conn net.Conn, serverConfig *ssh.ServerConfig) error {
	// Establish the initial connection.
	serverConn, serverChannels, serverReqs, err := ssh.NewServerConn(conn, serverConfig)
	if err != nil {
		return log.Errorf("Failed to establish new server connection with %s: %v", conn.RemoteAddr(), err)
	}
	defer serverConn.Close()
	go ssh.DiscardRequests(serverReqs)

	// Pop off the session information from our passwordCallback map immediately
	// as any new connections will pass through that callback and add it.
	remoteAddrString := conn.RemoteAddr().String()
	value, loaded := p.sessions.LoadAndDelete(remoteAddrString)
	if value == nil || !loaded {
		return log.Errorf("Unable to load session record for %q", remoteAddrString)
	}

	// Go find the resource via its UID.
	session_record, ok := value.(SessionRecord)
	if !ok {
		return log.Errorf("Critical error retrieving session record (invalid type conversion).")
	}
	resource, err := p.fish.ResourceGet(session_record.ResourceAccessor.ResourceUID)
	if err != nil {
		return log.Errorf("Unable to retrieve resource: %v", err)
	}

	// If the resource was not created with the authentication username and
	// password then they will both be the empty string, meaning we cannot
	// actually login to anything.
	if resource.Authentication.Username == "" && resource.Authentication.Password == "" {
		// TODO: in the future, this may not be an appropriate error.
		return log.Errorf("Resource Authentication not provided")
	}

	// Create the destination end of the proxy to the remote host.
	remoteAddr := fmt.Sprintf("%s:22", resource.IpAddr)
	remoteConfig := &ssh.ClientConfig{
		User: resource.Authentication.Username,
		Auth: []ssh.AuthMethod{
			ssh.Password(resource.Authentication.Password),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}
	remoteConn, err := ssh.Dial("tcp", remoteAddr, remoteConfig)
	if err != nil {
		return log.Errorf("Unable to dial proxy remote %q: %v", remoteAddr, err)
	}
	defer remoteConn.Close()

	// Open and connect the channels between the incoming connection and the
	// desired other end of the proxy (our 'remote').
	for newChannel := range serverChannels {
		remoteChannel, remoteRequests, remoteErr := remoteConn.OpenChannel(newChannel.ChannelType(), newChannel.ExtraData())
		if remoteErr != nil {
			return log.Errorf("Could not open channel to remote end of proxy: %v", remoteErr)
		}

		localChannel, localRequests, localErr := newChannel.Accept()
		if localErr != nil {
			return log.Errorf("Could not accept server channel: %v", localErr)
		}

		// Connect the requests.
		go func() {
			for {
				var request *ssh.Request
				var targetChannel ssh.Channel

				// This is the core "trick" about this infinite for loop: when
				// data is going back and forth one or both of these get hit
				// with an actual value.  However, when nothing is going between
				// this `select` will block unti la request comes in.
				select {
				case request = <-localRequests:
					targetChannel = remoteChannel
				case request = <-remoteRequests:
					targetChannel = localChannel
				}

				// In the event that an SSH request gets killed (not exited),
				// the request will be nil.  Do not continue, exit the loop.
				if request == nil {
					log.Warnf("SSH connection terminated ungracefully...")
					break
				}

				requestValid, requestError := targetChannel.SendRequest(request.Type, request.WantReply, request.Payload)
				if requestError != nil {
					log.Errorf("SendRequest error: %v", requestError)
					// TODO: we may need to identify a way to hit this error
					// in order to decide how to handle it.  For now, we
					// do nothing other than end the connection.
					// TODO: probably this is a good time to delete it?
					p.sessions.Delete(remoteAddrString)
					break
				}

				if request.WantReply {
					request.Reply(requestValid, nil)
				}

				// NOTE: currently we only care about the exit-status
				log.Debugf("Request: Type=%q, WantReply='%t'.", request.Type, request.WantReply)
				if request.Type == "exit-status" {
					// Connection is closed, end the proxy loop and remove this
					// session from the list of known sessions.
					p.sessions.Delete(remoteAddrString)
					// End the proxy loop
					break
				}
			}

			// End the communication between the local and remote.
			localChannel.Close()
			remoteChannel.Close()
		}()

		// Connect the channels.
		log.Debugf("Begin session connection between %q and %q.", conn.RemoteAddr(), remoteConn.RemoteAddr())
		go io.Copy(remoteChannel, localChannel)
		go io.Copy(localChannel, remoteChannel)
		// These are kept for safety to ensure the channels are indeed closed,
		// but in theory the ProxyLoop will close the channels and that will
		// signal to io.Copy that we are complete.
		defer localChannel.Close()
		defer remoteChannel.Close()
	}

	log.Debugf("Connection between %q and %q closed.", conn.RemoteAddr(), remoteConn.RemoteAddr())
	return nil
}

func (p *ProxyAccess) listenAndServe(address string) error {
	// Create the listener and let it wait for new connections in a separate
	// goroutine.
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return log.Errorf("net.Listen failed: %v", err)
	}
	defer listener.Close()

	// Indefinitely accept new connections, process them concurrently.
	for {
		conn, err := listener.Accept() // Blocks until new connection comes
		if err != nil {
			return log.Errorf("listen.Accept failed: %v", err)
		}

		go p.serveConnection(conn, p.serverConfig)
	}
}

func (p *ProxyAccess) passwordCallback(conn ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
	user := conn.User()
	log.Debugf("Login attempt from %q for user %q.", conn.RemoteAddr(), user)

	fishUser, err := p.fish.UserGet(user)
	if err != nil {
		log.Errorf("unrecognized user %q", user)
		return nil, fmt.Errorf("invalid access")
	}

	stringPass := string(pass)
	ra, err := p.fish.ResourceAccessSingleUsePassword(fishUser.Name, stringPass)
	if err != nil {
		// NOTE: do *NOT* return the database error to the user.
		log.Errorf("invalid access for user %q: %v", user, err)
		return nil, fmt.Errorf("invalid access")
	}

	// Only return non-error if the username and password match.
	if ra.Username == user && ra.Password == stringPass {
		remoteAddr := conn.RemoteAddr()
		// If the session is not already stored in our map, create it so that
		// we have access to it when processing the incoming connections.
		p.sessions.LoadOrStore(remoteAddr.String(), SessionRecord{ResourceAccessor: ra, RemoteAddr: remoteAddr})
		return nil, nil
	}

	// Otherwise, we have failed, return error to indicate as such.
	return nil, fmt.Errorf("invalid access")
}

func Init(fish *fish.Fish, id_rsa_path string, address string) error {
	// First, try and read the file if it exists already.  Otherwise, it is the
	// first execution, generate the private / public keys.  The SSH server
	// requires at least one identity loaded to run.
	privateBytes, err := os.ReadFile(id_rsa_path)
	if err != nil {
		// If it cannot be loaded, this is the first execution, generate it.
		log.Infof("SSH Proxy: could not load %q, generating now.", id_rsa_path)
		rsaKey, err := rsa.GenerateKey(rand.Reader, 4096)
		if err != nil {
			return fmt.Errorf("proxy_ssh: could not generate private key %q: %w", id_rsa_path, err)
		}
		pemKey := pem.EncodeToMemory(
			&pem.Block{
				Type:  "RSA PRIVATE KEY",
				Bytes: x509.MarshalPKCS1PrivateKey(rsaKey),
			},
		)
		// Write out the new key file and load into `privateBytes` again.
		if err := os.WriteFile(id_rsa_path, pemKey, 0600); err != nil {
			return fmt.Errorf("proxy_ssh: could not write %q: %w", id_rsa_path, err)
		}
		privateBytes, err = os.ReadFile(id_rsa_path)
		if err != nil {
			return fmt.Errorf(
				"proxy_ssh: failed to load private key %q after generating: %w",
				id_rsa_path,
				err,
			)
		}
	}

	private, err := ssh.ParsePrivateKey(privateBytes)
	if err != nil {
		return fmt.Errorf("proxy_ssh: failed to parse private key: %w", err)
	}

	ssh_proxy := ProxyAccess{fish: fish}
	ssh_proxy.serverConfig = &ssh.ServerConfig{
		PasswordCallback: ssh_proxy.passwordCallback,
	}
	ssh_proxy.serverConfig.AddHostKey(private)

	go ssh_proxy.listenAndServe(address)

	return nil
}
