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

// Simplifies work with ssh testing
package helper

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"unsafe"

	"github.com/creack/pty"
	sshd "github.com/gliderlabs/ssh"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// Base ssh server with no handler
func MockSSHServer(t *testing.T, sshSrv *sshd.Server, user, pass, key string) (string, string) {
	t.Helper()
	if pass != "" {
		sshSrv.SetOption(sshd.PasswordAuth(func(ctx sshd.Context, password string) bool {
			res := ctx.User() == user && password == pass
			t.Log("MockSSHServer: Checked password:", res)
			return res
		}))
	}
	if key != "" {
		sshSrv.SetOption(sshd.PublicKeyAuth(func(ctx sshd.Context, inkey sshd.PublicKey) bool {
			res := ctx.User() == user && key == string(ssh.MarshalAuthorizedKey(inkey))
			t.Log("MockSSHServer: Checked pubkey:", res)
			return res
		}))
	}

	sshListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("MockSSHServer: Unable to start SSH server to listen: %v", err)
		return "", ""
	}
	t.Cleanup(func() {
		sshListener.Close()
	})
	sshSrv.Addr = sshListener.Addr().String()

	_, port, err := net.SplitHostPort(sshListener.Addr().String())
	if err != nil {
		t.Fatalf("MockSSHServer: Unable to get SSH listening port: %v", err)
		return "", ""
	}

	go sshSrv.Serve(sshListener)

	t.Log("MockSSHServer: Started Test SSH server on", sshSrv.Addr)

	return "127.0.0.1", port
}

func MockSSHPtyServer(t *testing.T, user, pass, key string) (string, string) {
	t.Helper()
	sshSrv := &sshd.Server{Handler: func(s sshd.Session) {
		t.Log("MockSSHPtyServer: Start handling session")
		cmd := exec.Command("sh")
		ptyReq, winCh, isPty := s.Pty()
		if isPty {
			t.Log("MockSSHPtyServer: PTY is requested")
			cmd.Env = append(cmd.Env, fmt.Sprintf("TERM=%s", ptyReq.Term))
			f, err := pty.Start(cmd)
			if err != nil {
				panic(err)
			}
			go func() {
				for win := range winCh {
					setWinsize(f, win.Width, win.Height)
				}
			}()
			go func() {
				t.Log("MockSSHPtyServer: starting session->cmd copy")
				io.Copy(f, s) // stdin
				t.Log("MockSSHPtyServer: ended session->cmd copy")
			}()
			t.Log("MockSSHPtyServer: starting cmd->session copy")
			io.Copy(s, f) // stdout
			t.Log("MockSSHPtyServer: ended cmd->session copy")
			cmd.Wait()
		} else {
			t.Log("MockSSHPtyServer: No PTY is requested")
			io.WriteString(s, "No PTY requested.\n")
			s.Exit(1)
		}
		t.Log("MockSSHPtyServer: End handling session")
	}}
	return MockSSHServer(t, sshSrv, user, pass, key)
}

func setWinsize(f *os.File, w, h int) {
	syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), uintptr(syscall.TIOCSWINSZ),
		uintptr(unsafe.Pointer(&struct{ h, w, x, y uint16 }{uint16(h), uint16(w), 0, 0})))
}

func MockSSHSftpServer(t *testing.T, user, pass, key string) (string, string) {
	t.Helper()
	sshSrv := &sshd.Server{
		SubsystemHandlers: map[string]sshd.SubsystemHandler{
			"sftp": func(s sshd.Session) {
				t.Log("MockSSHSftpServer: Start handling session")
				debugStream := os.Stderr
				serverOptions := []sftp.ServerOption{
					sftp.WithDebug(debugStream),
				}
				server, err := sftp.NewServer(s, serverOptions...)
				if err != nil {
					t.Log("MockSSHSftpServer: Init error:", err)
					return
				}
				if err := server.Serve(); err == io.EOF {
					server.Close()
					t.Log("MockSSHSftpServer: Slient exited session.")
				} else if err != nil {
					t.Log("MockSSHSftpServer: Server completed with error:", err)
				}
				t.Log("MockSSHSftpServer: End handling session")
			},
		},
	}
	return MockSSHServer(t, sshSrv, user, pass, key)
}

func MockSSHPortServer(t *testing.T, user, pass, key string) (string, string) {
	t.Helper()
	forwardHandler := &sshd.ForwardedTCPHandler{}

	sshSrv := &sshd.Server{
		Handler: sshd.Handler(func(s sshd.Session) {
			io.WriteString(s, "Remote forwarding available...\n")
			select {}
		}),
		LocalPortForwardingCallback: sshd.LocalPortForwardingCallback(func(_ sshd.Context, dhost string, dport uint32) bool {
			t.Log("Accepted forward", dhost, dport)
			return true
		}),
		ReversePortForwardingCallback: sshd.ReversePortForwardingCallback(func(_ sshd.Context, host string, port uint32) bool {
			t.Log("Attempt to bind", host, port, "granted")
			return true
		}),
		RequestHandlers: map[string]sshd.RequestHandler{
			"tcpip-forward":        forwardHandler.HandleSSHRequest,
			"cancel-tcpip-forward": forwardHandler.HandleSSHRequest,
		},
		ChannelHandlers: map[string]sshd.ChannelHandler{
			"direct-tcpip": sshd.DirectTCPIPHandler,
		},
	}
	return MockSSHServer(t, sshSrv, user, pass, key)
}

func RunCmdPtySSH(addr, username, password, cmd string) ([]byte, error) {
	cfg := &ssh.ClientConfig{
		User: username,
		Auth: []ssh.AuthMethod{
			ssh.Password(password),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // #nosec G106 , tests need to be simple
	}

	conn, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		return nil, fmt.Errorf("RunCmdPtySSH: Unable to connect to %s: %v", addr, err)
	}

	session, err := conn.NewSession()
	if err != nil {
		return nil, fmt.Errorf("RunCmdPtySSH: Unable to create session: %v", err)
	}
	defer session.Close()

	// Set up terminal modes
	modes := ssh.TerminalModes{
		ssh.ECHO:          0,     // disable echoing
		ssh.TTY_OP_ISPEED: 14400, // input speed = 14.4kbaud
		ssh.TTY_OP_OSPEED: 14400, // output speed = 14.4kbaud
	}
	// Request pseudo terminal
	if err := session.RequestPty("xterm", 40, 80, modes); err != nil {
		return nil, fmt.Errorf("RunCmdPtySSH: Unable to request PTY: %v", err)
	}

	// Set up standard input/output
	stdin, err := session.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("RunCmdPtySSH: Unable to get session stdin: %v", err)
	}

	stdout, err := session.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("RunCmdPtySSH: Unable to get session stdout: %v", err)
	}

	// Start remote shell
	if err := session.Shell(); err != nil {
		return nil, fmt.Errorf("RunCmdPtySSH: Unable to request shell: %v", err)
	}

	// Send command
	if _, err = io.WriteString(stdin, fmt.Sprintf("%s\n", cmd)); err != nil {
		return nil, fmt.Errorf("RunCmdPtySSH: Unable to write to stdin: %v", err)
	}
	// Send exit to shell
	if _, err = io.WriteString(stdin, "exit\n"); err != nil {
		return nil, fmt.Errorf("RunCmdPtySSH: Unable to write to stdin: %v", err)
	}

	return io.ReadAll(stdout)
}

// SCP nowadays uses sftp subsystem with no need for scp binary on the target, so use it directly
func RunSftp(addr, username, password string, files []string, toPath string, toRemote bool) error {
	cfg := &ssh.ClientConfig{
		User: username,
		Auth: []ssh.AuthMethod{
			ssh.Password(password),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // #nosec G106 , tests need to be simple
	}

	conn, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		return fmt.Errorf("RunSftp: Unable to connect to %s: %v", addr, err)
	}

	client, err := sftp.NewClient(conn)
	if err != nil {
		return fmt.Errorf("RunSftp: Unable to create sftp client: %v", err)
	}
	defer client.Close()

	for _, path := range files {
		if toRemote {
			err = sftpToRemote(client, path, filepath.Join(toPath, filepath.Base(path)))
		} else {
			err = sftpFromRemote(client, path, filepath.Join(toPath, filepath.Base(path)))
		}
		if err != nil {
			return fmt.Errorf("RunSftp: %v", err)
		}
	}

	return nil
}

func sftpToRemote(client *sftp.Client, srcPath, dstPath string) error {
	localFile, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("Unable to open a local source file %q: %v", srcPath, err)
	}
	defer localFile.Close()

	remoteFile, err := client.Create(dstPath)
	if err != nil {
		return fmt.Errorf("Unable to create a remote destination file %q: %v", dstPath, err)
	}
	defer remoteFile.Close()

	if _, err = localFile.WriteTo(remoteFile); err != nil {
		return fmt.Errorf("Unable to copy local to remote file: %v", err)
	}

	return nil
}

func sftpFromRemote(client *sftp.Client, srcPath, dstPath string) error {
	remoteFile, err := client.Open(srcPath)
	if err != nil {
		return fmt.Errorf("Unable to open a remote destination file %q: %v", srcPath, err)
	}
	defer remoteFile.Close()

	localFile, err := os.Create(dstPath)
	if err != nil {
		return fmt.Errorf("Unable to create a local destination file %q: %v", dstPath, err)
	}
	defer localFile.Close()

	if _, err = remoteFile.WriteTo(localFile); err != nil {
		return fmt.Errorf("Unable to copy remote to local file: %v", err)
	}

	return nil
}
