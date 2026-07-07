// Copyright (c) the go-ruby-capistrano/capistrano authors
//
// SPDX-License-Identifier: BSD-3-Clause

package capistrano

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

// --- sshBackend orchestration, via a fake Dialer (no network) ----------------

type fakeConn struct {
	res    CommandResult
	err    error
	closed bool
}

func (c *fakeConn) Run(string) (CommandResult, error) { return c.res, c.err }
func (c *fakeConn) Close() error                      { c.closed = true; return nil }

type fakeDialer struct {
	conn *fakeConn
	err  error
}

func (d fakeDialer) Dial(*Server) (SSHConn, error) {
	if d.err != nil {
		return nil, d.err
	}
	return d.conn, nil
}

func TestSSHBackendRunViaFakeDialer(t *testing.T) {
	host := NewServer("web1")

	// success path: result flows through, connection is closed
	fc := &fakeConn{res: CommandResult{Stdout: "hi", ExitStatus: 0}}
	b := newSSHBackendWithDialer(fakeDialer{conn: fc})
	res, err := b.Run(host, "echo hi")
	if err != nil || res.Stdout != "hi" {
		t.Fatalf("Run = %+v err=%v", res, err)
	}
	if !fc.closed {
		t.Fatal("connection not closed")
	}

	// dial failure surfaces as a transport error
	dialErr := errors.New("dial refused")
	b2 := newSSHBackendWithDialer(fakeDialer{err: dialErr})
	if _, err := b2.Run(host, "x"); err != dialErr {
		t.Fatalf("dial error = %v", err)
	}
}

func TestSSHBackendTransfersDeferred(t *testing.T) {
	b := newSSHBackendWithDialer(fakeDialer{conn: &fakeConn{}})
	host := NewServer("web1")
	if err := b.Upload(host, "a", "b"); err == nil {
		t.Fatal("Upload should report deferred")
	}
	if err := b.Download(host, "b", "a"); err == nil {
		t.Fatal("Download should report deferred")
	}
}

func TestNewSSHBackendConstruct(t *testing.T) {
	b := NewSSHBackend(&ssh.ClientConfig{HostKeyCallback: ssh.InsecureIgnoreHostKey()})
	if b == nil {
		t.Fatal("NewSSHBackend nil")
	}
}

// --- Real sshDialer / sshConn, via an in-process loopback SSH server ---------

// execHandler decides one command's output on the loopback server.
type execHandler func(cmd string) (stdout, stderr string, exit int, sendExit bool)

// startSSHServer boots a one-key SSH server on loopback, accepting any password
// and running handler for each exec request. It returns the listen address; the
// listener is closed at test end.
func startSSHServer(t *testing.T, handler execHandler) string {
	t.Helper()

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ssh.ServerConfig{
		PasswordCallback: func(ssh.ConnMetadata, []byte) (*ssh.Permissions, error) { return nil, nil },
	}
	cfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			nConn, err := ln.Accept()
			if err != nil {
				return
			}
			go serveConn(nConn, cfg, handler)
		}
	}()
	return ln.Addr().String()
}

func serveConn(nConn net.Conn, cfg *ssh.ServerConfig, handler execHandler) {
	sConn, chans, reqs, err := ssh.NewServerConn(nConn, cfg)
	if err != nil {
		return
	}
	defer sConn.Close()
	go ssh.DiscardRequests(reqs)
	for newChan := range chans {
		if newChan.ChannelType() != "session" {
			newChan.Reject(ssh.UnknownChannelType, "only session")
			continue
		}
		ch, chReqs, err := newChan.Accept()
		if err != nil {
			return
		}
		go handleSession(ch, chReqs, handler)
	}
}

func handleSession(ch ssh.Channel, reqs <-chan *ssh.Request, handler execHandler) {
	for req := range reqs {
		if req.Type != "exec" {
			req.Reply(false, nil)
			continue
		}
		var payload struct{ Command string }
		ssh.Unmarshal(req.Payload, &payload)
		req.Reply(true, nil)

		stdout, stderr, exit, sendExit := handler(payload.Command)
		io.WriteString(ch, stdout)
		io.WriteString(ch.Stderr(), stderr)
		if sendExit {
			ch.SendRequest("exit-status", false,
				ssh.Marshal(struct{ Status uint32 }{uint32(exit)}))
		}
		ch.Close()
		return
	}
}

func loopbackClientConfig() *ssh.ClientConfig {
	return &ssh.ClientConfig{
		User:            "deploy",
		Auth:            []ssh.AuthMethod{ssh.Password("secret")},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}
}

func serverFor(addr string) *Server {
	host, port, _ := net.SplitHostPort(addr)
	return NewServer(host + ":" + port)
}

func TestSSHDialerHappyPath(t *testing.T) {
	addr := startSSHServer(t, func(cmd string) (string, string, int, bool) {
		if cmd != "echo hello" {
			return "", "unexpected", 1, true
		}
		return "hello\n", "", 0, true
	})
	b := NewSSHBackend(loopbackClientConfig())
	res, err := b.Run(serverFor(addr), "echo hello")
	if err != nil {
		t.Fatalf("Run err = %v", err)
	}
	if res.Stdout != "hello\n" || res.ExitStatus != 0 {
		t.Fatalf("Run result = %+v", res)
	}
}

func TestSSHConnNonZeroExit(t *testing.T) {
	addr := startSSHServer(t, func(string) (string, string, int, bool) {
		return "", "boom\n", 3, true // clean non-zero exit → *ssh.ExitError
	})
	b := NewSSHBackend(loopbackClientConfig())
	res, err := b.Run(serverFor(addr), "false")
	if err != nil {
		t.Fatalf("non-zero exit should not be a transport error: %v", err)
	}
	if res.ExitStatus != 3 || res.Stderr != "boom\n" {
		t.Fatalf("result = %+v", res)
	}
}

func TestSSHConnMissingExitIsTransportError(t *testing.T) {
	addr := startSSHServer(t, func(string) (string, string, int, bool) {
		return "partial", "", 0, false // channel closes with no exit-status
	})
	b := NewSSHBackend(loopbackClientConfig())
	_, err := b.Run(serverFor(addr), "cmd")
	if err == nil {
		t.Fatal("missing exit-status should be a transport error")
	}
}

func TestSSHConnNewSessionAfterCloseErrors(t *testing.T) {
	addr := startSSHServer(t, func(string) (string, string, int, bool) {
		return "", "", 0, true
	})
	d := sshDialer{config: loopbackClientConfig()}
	conn, err := d.Dial(serverFor(addr))
	if err != nil {
		t.Fatal(err)
	}
	sc := conn.(*sshConn)
	if err := sc.Close(); err != nil {
		t.Fatal(err)
	}
	// opening a session on the closed client fails (NewSession error branch)
	if _, err := sc.Run("x"); err == nil {
		t.Fatal("Run on closed connection should error")
	}
}

func TestSSHDialerDialError(t *testing.T) {
	// bind then immediately close a listener to obtain a refused address
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()

	b := NewSSHBackend(loopbackClientConfig())
	if _, err := b.Run(serverFor(addr), "x"); err == nil {
		t.Fatal("dial to closed port should error")
	} else if !strings.Contains(err.Error(), "connect") && !strings.Contains(err.Error(), "refused") &&
		!strings.Contains(err.Error(), "dial") {
		// message varies by OS; just ensure it is a real dial failure
		t.Logf("dial error (ok): %v", err)
	}
}
