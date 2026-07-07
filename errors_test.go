// Copyright (c) the go-ruby-capistrano/capistrano authors
//
// SPDX-License-Identifier: BSD-3-Clause

package capistrano

import (
	"strings"
	"testing"
)

func TestErrorTree(t *testing.T) {
	// every error type joins the sealed tree
	tree := []CapistranoError{
		&Error{Message: "x"},
		&TaskNotFoundError{Name: "t"},
		&NoMatchingServersError{},
		&CommandError{},
	}
	for _, e := range tree {
		e.isCapistranoError() // marker (sealed hierarchy)
		if e.Error() == "" && (e.(interface{ isCapistranoError() }) == nil) {
			t.Fatal("unreachable")
		}
	}

	if got := (&Error{Message: "boom"}).Error(); got != "boom" {
		t.Fatalf("Error = %q", got)
	}
	if got := (&TaskNotFoundError{Name: "deploy"}).Error(); !strings.Contains(got, "deploy") {
		t.Fatalf("TaskNotFoundError = %q", got)
	}
}

func TestNoMatchingServersErrorMessages(t *testing.T) {
	if got := (&NoMatchingServersError{}).Error(); !strings.Contains(got, "at least one") {
		t.Fatalf("empty filter = %q", got)
	}
	if got := (&NoMatchingServersError{Filter: "web"}).Error(); !strings.Contains(got, "web") {
		t.Fatalf("filtered = %q", got)
	}
}

func TestCommandErrorMessages(t *testing.T) {
	// with stderr and host
	e := &CommandError{Host: "web1", Command: "ls /x", ExitStatus: 2, Stderr: "no such file\n"}
	m := e.Error()
	for _, want := range []string{"ls /x exit status: 2", "stderr: no such file", "on web1"} {
		if !strings.Contains(m, want) {
			t.Fatalf("CommandError missing %q in %q", want, m)
		}
	}
	// empty stderr and no host → "Nothing written", no "on" line
	e2 := &CommandError{Command: "true", ExitStatus: 1}
	m2 := e2.Error()
	if !strings.Contains(m2, "stderr: Nothing written") {
		t.Fatalf("empty stderr = %q", m2)
	}
	if strings.Contains(m2, "\non ") {
		t.Fatalf("unexpected host line = %q", m2)
	}
}
