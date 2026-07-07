// Copyright (c) the go-ruby-capistrano/capistrano authors
//
// SPDX-License-Identifier: BSD-3-Clause

package capistrano

import (
	"errors"
	"testing"
)

func TestSessionExecuteSemantics(t *testing.T) {
	host := NewServer("web1")
	fb := NewFakeBackend()
	s := &Session{backend: fb, host: host}

	if s.Host() != host {
		t.Fatal("Host mismatch")
	}

	// execute success (no args → command used verbatim)
	if err := s.Execute("true"); err != nil {
		t.Fatalf("Execute(true) = %v", err)
	}

	// execute non-zero exit → CommandError
	fb.Script("false", CommandResult{ExitStatus: 1, Stderr: "nope"}, nil)
	var ce *CommandError
	if err := s.Execute("false"); !errors.As(err, &ce) {
		t.Fatalf("Execute(false) = %v, want CommandError", err)
	}

	// execute transport error surfaces
	tErr := errors.New("dial down")
	fb2 := NewFakeBackend().FailTransport(tErr)
	s2 := &Session{backend: fb2, host: host}
	if err := s2.Execute("x"); err != tErr {
		t.Fatalf("Execute transport = %v", err)
	}
}

func TestSessionTest(t *testing.T) {
	host := NewServer("web1")
	fb := NewFakeBackend()
	s := &Session{backend: fb, host: host}

	// exit 0 → true
	ok, err := s.Test("test", "-d", "/tmp")
	if err != nil || !ok {
		t.Fatalf("Test true = %v %v", ok, err)
	}
	// non-zero → false, no error
	fb.Script("test -d /missing", CommandResult{ExitStatus: 1}, nil)
	ok, err = s.Test("test", "-d", "/missing")
	if err != nil || ok {
		t.Fatalf("Test false = %v %v", ok, err)
	}
	// transport error
	fb3 := NewFakeBackend().FailTransport(errors.New("down"))
	s3 := &Session{backend: fb3, host: host}
	if _, err := s3.Test("x"); err == nil {
		t.Fatal("Test transport should error")
	}
}

func TestSessionCapture(t *testing.T) {
	host := NewServer("web1")
	fb := NewFakeBackend()
	fb.Script("cat file", CommandResult{Stdout: "  hello \n"}, nil)
	s := &Session{backend: fb, host: host}

	// capture trims stdout
	out, err := s.Capture("cat", "file")
	if err != nil || out != "hello" {
		t.Fatalf("Capture = %q %v", out, err)
	}
	// capture non-zero → CommandError
	fb.Script("boom", CommandResult{ExitStatus: 5, Stderr: "bad"}, nil)
	var ce *CommandError
	if _, err := s.Capture("boom"); !errors.As(err, &ce) {
		t.Fatalf("Capture failure = %v", err)
	}
	// capture transport error
	fb3 := NewFakeBackend().FailTransport(errors.New("down"))
	s3 := &Session{backend: fb3, host: host}
	if _, err := s3.Capture("x"); err == nil {
		t.Fatal("Capture transport should error")
	}
}

func TestSessionTransfers(t *testing.T) {
	host := NewServer("web1")
	fb := NewFakeBackend()
	s := &Session{backend: fb, host: host}

	if err := s.Upload("local.txt", "/remote.txt"); err != nil {
		t.Fatalf("Upload = %v", err)
	}
	if err := s.Download("/remote.log", "local.log"); err != nil {
		t.Fatalf("Download = %v", err)
	}
	if len(fb.Uploads) != 1 || fb.Uploads[0] != "local.txt -> /remote.txt" {
		t.Fatalf("Uploads = %v", fb.Uploads)
	}
	if len(fb.Downloads) != 1 || fb.Downloads[0] != "/remote.log -> local.log" {
		t.Fatalf("Downloads = %v", fb.Downloads)
	}

	// scripted transfer failures
	upErr := errors.New("upload failed")
	dnErr := errors.New("download failed")
	fb2 := NewFakeBackend().FailUploads(upErr).FailDownloads(dnErr)
	s2 := &Session{backend: fb2, host: host}
	if err := s2.Upload("a", "b"); err != upErr {
		t.Fatalf("Upload failure = %v", err)
	}
	if err := s2.Download("b", "a"); err != dnErr {
		t.Fatalf("Download failure = %v", err)
	}
}

// TestApplicationDefaults exercises the default input and release-timestamp
// seams (the closures NewApplication installs) without overriding them.
func TestApplicationDefaults(t *testing.T) {
	// default input returns "", so ask falls back to its default
	app := NewApplication()
	app.Ask("token", "token?", "fallback")
	if got := app.Fetch("token"); got != "fallback" {
		t.Fatalf("default-input ask = %v", got)
	}

	// default release timestamp names the release directory
	app2 := NewApplication()
	fb := NewFakeBackend()
	app2.SetBackend(fb)
	app2.Set("repo_url", "repo")
	app2.Role("web", []string{"web1"}, nil)
	if err := app2.Invoke("deploy:updating"); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, cmd := range fb.Commands {
		if cmd == "mkdir -p /var/www/app/releases/20260707000000" {
			found = true
		}
	}
	if !found {
		t.Fatalf("default release timestamp not used: %v", fb.Commands)
	}
}
