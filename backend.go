// Copyright (c) the go-ruby-capistrano/capistrano authors
//
// SPDX-License-Identifier: BSD-3-Clause

package capistrano

import "strings"

// CommandResult is the outcome of running one shell command on one host — the
// pure-data slice of an SSHKit::Command after it has run. ExitStatus is the
// process exit code (0 on success); Stdout / Stderr are the captured streams. A
// non-zero ExitStatus is NOT a transport error — the Backend reports it here and
// the Session decides whether to raise (execute / capture do; test does not).
type CommandResult struct {
	Stdout     string
	Stderr     string
	ExitStatus int
}

// Backend is the injectable command-execution seam — the SSHKit backend behind
// Capistrano's on(hosts) { … } context. Run executes one command on one host
// and reports the CommandResult; a returned error means the command could not be
// run at all (dial/transport failure), distinct from a non-zero exit. Upload and
// Download transfer a file to/from the host. The default is a real SSH backend
// (see NewSSHBackend); tests inject a FakeBackend.
type Backend interface {
	Run(host *Server, command string) (CommandResult, error)
	Upload(host *Server, local, remote string) error
	Download(host *Server, remote, local string) error
}

// Session is the per-host execution context yielded to an on-block — the port of
// SSHKit's backend receiver. Its execute / test / capture reproduce SSHKit's
// semantics faithfully: execute raises on a non-zero exit, test returns a
// boolean, capture returns stripped stdout (and raises on a non-zero exit).
type Session struct {
	backend Backend
	host    *Server
}

// Host returns the server this session targets (the block's `host`).
func (s *Session) Host() *Server { return s.host }

// buildCommand joins a command and its argument tokens with spaces — the
// SSHKit command builder (`execute :git, :clone, url` → "git clone url").
func buildCommand(command string, args ...string) string {
	if len(args) == 0 {
		return command
	}
	return command + " " + strings.Join(args, " ")
}

// Execute runs command and raises a CommandError if it exits non-zero — the
// port of SSHKit's `execute` (and Capistrano's `execute`/`execute!`). It returns
// nil on success and the transport error if the command could not be run.
func (s *Session) Execute(command string, args ...string) error {
	line := buildCommand(command, args...)
	res, err := s.backend.Run(s.host, line)
	if err != nil {
		return err
	}
	if res.ExitStatus != 0 {
		return &CommandError{
			Host:       s.host.String(),
			Command:    line,
			ExitStatus: res.ExitStatus,
			Stderr:     res.Stderr,
		}
	}
	return nil
}

// Test runs command and reports whether it exited zero — the port of SSHKit's
// `test` (a boolean predicate that never raises on a non-zero exit). A transport
// failure is still returned as an error.
func (s *Session) Test(command string, args ...string) (bool, error) {
	res, err := s.backend.Run(s.host, buildCommand(command, args...))
	if err != nil {
		return false, err
	}
	return res.ExitStatus == 0, nil
}

// Capture runs command and returns its stripped stdout — the port of SSHKit's
// `capture`. Like execute it raises a CommandError on a non-zero exit.
func (s *Session) Capture(command string, args ...string) (string, error) {
	line := buildCommand(command, args...)
	res, err := s.backend.Run(s.host, line)
	if err != nil {
		return "", err
	}
	if res.ExitStatus != 0 {
		return "", &CommandError{
			Host:       s.host.String(),
			Command:    line,
			ExitStatus: res.ExitStatus,
			Stderr:     res.Stderr,
		}
	}
	return strings.TrimSpace(res.Stdout), nil
}

// Upload transfers local to remote on the host, raising on failure — the port of
// `upload!`.
func (s *Session) Upload(local, remote string) error {
	return s.backend.Upload(s.host, local, remote)
}

// Download transfers remote to local from the host, raising on failure — the
// port of `download!`.
func (s *Session) Download(remote, local string) error {
	return s.backend.Download(s.host, remote, local)
}

// FakeBackend is an in-process, deterministic Backend for tests — the port of
// SSHKit's test backend. It records every command / upload / download and
// returns scripted CommandResults keyed by the command string (falling back to
// a zero-exit empty result, so a plain command "succeeds" unless scripted
// otherwise). It performs no I/O and needs no network, so the suite is
// reproducible on every architecture.
type FakeBackend struct {
	// Commands is the ordered log of every command Run received.
	Commands []string
	// Uploads / Downloads log every transfer as "local -> remote".
	Uploads   []string
	Downloads []string

	// results scripts the CommandResult (and optional transport error) returned
	// for a given command string.
	results map[string]fakeResult
	// runErr, when set, is returned as the transport error for every command.
	runErr error
	// uploadErr / downloadErr, when set, fail every transfer.
	uploadErr   error
	downloadErr error
}

type fakeResult struct {
	res CommandResult
	err error
}

// NewFakeBackend returns an empty fake backend.
func NewFakeBackend() *FakeBackend {
	return &FakeBackend{results: map[string]fakeResult{}}
}

// Script makes the fake return res (and err as the transport error) whenever the
// exact command line is run. It returns the backend for chaining.
func (f *FakeBackend) Script(command string, res CommandResult, err error) *FakeBackend {
	f.results[command] = fakeResult{res: res, err: err}
	return f
}

// FailTransport makes every Run report err as a transport failure (dial down).
func (f *FakeBackend) FailTransport(err error) *FakeBackend {
	f.runErr = err
	return f
}

// FailUploads / FailDownloads make every transfer of that kind fail.
func (f *FakeBackend) FailUploads(err error) *FakeBackend   { f.uploadErr = err; return f }
func (f *FakeBackend) FailDownloads(err error) *FakeBackend { f.downloadErr = err; return f }

// Run records command and returns its scripted result (or a zero-exit empty
// result when unscripted), honouring a global transport failure.
func (f *FakeBackend) Run(host *Server, command string) (CommandResult, error) {
	f.Commands = append(f.Commands, command)
	if f.runErr != nil {
		return CommandResult{}, f.runErr
	}
	if r, ok := f.results[command]; ok {
		return r.res, r.err
	}
	return CommandResult{}, nil
}

// Upload records the transfer and honours a scripted upload failure.
func (f *FakeBackend) Upload(host *Server, local, remote string) error {
	f.Uploads = append(f.Uploads, local+" -> "+remote)
	return f.uploadErr
}

// Download records the transfer and honours a scripted download failure.
func (f *FakeBackend) Download(host *Server, remote, local string) error {
	f.Downloads = append(f.Downloads, remote+" -> "+local)
	return f.downloadErr
}
