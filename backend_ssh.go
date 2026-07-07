// Copyright (c) the go-ruby-capistrano/capistrano authors
//
// SPDX-License-Identifier: BSD-3-Clause

package capistrano

import (
	"bytes"

	"golang.org/x/crypto/ssh"
)

// SSHConn is one live SSH connection to a host — the small surface the SSH
// backend needs from golang.org/x/crypto/ssh. It is an interface so the backend
// orchestration is testable with an in-process fake, while the real
// implementation (sshConn) wraps *ssh.Client.
type SSHConn interface {
	// Run executes command over the connection and reports the CommandResult. A
	// non-zero exit is returned in the result (ExitStatus), not as an error; a
	// returned error is a transport/protocol failure.
	Run(command string) (CommandResult, error)
	// Close tears the connection down.
	Close() error
}

// Dialer opens an SSHConn to a host — the injectable seam under the SSH backend.
// The default (sshDialer) dials over the network with x/crypto/ssh; tests inject
// a fake, and an in-process ssh server exercises the default.
type Dialer interface {
	Dial(host *Server) (SSHConn, error)
}

// sshBackend is the default Backend: it dials each host through a Dialer, runs
// the command over the returned connection, and closes it. All of its logic is
// seam-driven, so it is fully testable without a real network.
type sshBackend struct {
	dialer Dialer
}

// NewSSHBackend returns the default real-SSH Backend using config to
// authenticate. It is the Backend NewApplication installs; a deploy that never
// injects a different Backend uses this one to reach real hosts over
// golang.org/x/crypto/ssh.
func NewSSHBackend(config *ssh.ClientConfig) Backend {
	return &sshBackend{dialer: sshDialer{config: config}}
}

// newSSHBackendWithDialer builds an SSH backend over an arbitrary Dialer (the
// seam tests inject a fake through).
func newSSHBackendWithDialer(d Dialer) Backend { return &sshBackend{dialer: d} }

// Run dials the host, runs command, and closes the connection.
func (b *sshBackend) Run(host *Server, command string) (CommandResult, error) {
	conn, err := b.dialer.Dial(host)
	if err != nil {
		return CommandResult{}, err
	}
	defer conn.Close()
	return conn.Run(command)
}

// Upload over raw SSH is deferred (it needs an SFTP/SCP sub-channel); the seam
// is fully present so a host can inject a transferring Backend. See the README
// "Deferred" note.
func (b *sshBackend) Upload(host *Server, local, remote string) error {
	return &Error{Message: "upload! over the default SSH backend is not implemented (deferred; inject a Backend)"}
}

// Download over raw SSH is deferred, symmetrically with Upload.
func (b *sshBackend) Download(host *Server, remote, local string) error {
	return &Error{Message: "download! over the default SSH backend is not implemented (deferred; inject a Backend)"}
}

// sshDialer is the default Dialer, dialing over TCP with x/crypto/ssh.
type sshDialer struct {
	config *ssh.ClientConfig
}

// Dial opens a real SSH connection to host.HostPort() and wraps it as an
// SSHConn. This is the one edge that touches the network; the in-process ssh
// server test drives it.
func (d sshDialer) Dial(host *Server) (SSHConn, error) {
	client, err := ssh.Dial("tcp", host.HostPort(), d.config)
	if err != nil {
		return nil, err
	}
	return &sshConn{client: client}, nil
}

// sshConn adapts an *ssh.Client to SSHConn.
type sshConn struct {
	client *ssh.Client
}

// Run opens a session, runs command, and captures stdout/stderr and the exit
// status. A clean non-zero exit (*ssh.ExitError) becomes a CommandResult with
// that status and no error; any other failure is a transport error.
func (c *sshConn) Run(command string) (CommandResult, error) {
	session, err := c.client.NewSession()
	if err != nil {
		return CommandResult{}, err
	}
	defer session.Close()

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	runErr := session.Run(command)
	res := CommandResult{Stdout: stdout.String(), Stderr: stderr.String()}
	if runErr != nil {
		if exit, ok := runErr.(*ssh.ExitError); ok {
			res.ExitStatus = exit.ExitStatus()
			return res, nil
		}
		return res, runErr
	}
	return res, nil
}

// Close closes the underlying client.
func (c *sshConn) Close() error { return c.client.Close() }
