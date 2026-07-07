// Copyright (c) the go-ruby-capistrano/capistrano authors
//
// SPDX-License-Identifier: BSD-3-Clause

package capistrano

import (
	"fmt"
	"strings"
)

// CapistranoError is the root of the Capistrano error tree (Capistrano::Error <
// StandardError). Every error this package raises satisfies it, so a host (rbgo)
// can map the whole family onto a single Ruby exception class and still switch
// on the concrete Go type for the specific subclass.
type CapistranoError interface {
	error
	// isCapistranoError is an unexported marker so only this package's error
	// types can join the tree (the sealed-hierarchy idiom).
	isCapistranoError()
}

// Error is a plain Capistrano::Error carrying a message — the base node of the
// tree, raised where MRI would raise Capistrano::Error itself (e.g. an unknown
// stage, a circular task dependency).
type Error struct{ Message string }

func (e *Error) Error() string      { return e.Message }
func (e *Error) isCapistranoError() { _ = e }

// TaskNotFoundError is raised when invoke targets a name no task is registered
// under — the port of the "Don't know how to build task" failure Rake raises
// through Capistrano's DSL.
type TaskNotFoundError struct{ Name string }

func (e *TaskNotFoundError) Error() string {
	return fmt.Sprintf("Don't know how to build task '%s'", e.Name)
}
func (e *TaskNotFoundError) isCapistranoError() { _ = e }

// NoMatchingServersError is Capistrano::NoMatchingServersError — raised when an
// action needs at least one server but the role/property filter selected none.
type NoMatchingServersError struct{ Filter string }

func (e *NoMatchingServersError) Error() string {
	if e.Filter == "" {
		return "capistrano requires at least one matching server"
	}
	return fmt.Sprintf("capistrano requires at least one matching server for %s", e.Filter)
}
func (e *NoMatchingServersError) isCapistranoError() { _ = e }

// CommandError is the port of SSHKit::Command::Failed — raised when execute /
// capture run a command that exits non-zero. It carries the host, the command
// line, the exit status, and the captured stderr so the message reproduces
// SSHKit's multi-line report.
type CommandError struct {
	Host       string
	Command    string
	ExitStatus int
	Stderr     string
}

func (e *CommandError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s exit status: %d", e.Command, e.ExitStatus)
	fmt.Fprintf(&b, "\n%s stdout: Nothing written", e.Command)
	if s := strings.TrimRight(e.Stderr, "\n"); s != "" {
		fmt.Fprintf(&b, "\n%s stderr: %s", e.Command, s)
	} else {
		fmt.Fprintf(&b, "\n%s stderr: Nothing written", e.Command)
	}
	if e.Host != "" {
		fmt.Fprintf(&b, "\non %s", e.Host)
	}
	return b.String()
}
func (e *CommandError) isCapistranoError() { _ = e }
