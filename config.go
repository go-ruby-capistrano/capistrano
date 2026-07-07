// Copyright (c) the go-ruby-capistrano/capistrano authors
//
// SPDX-License-Identifier: BSD-3-Clause

// Package capistrano is a pure-Go (no cgo) port of the core of Ruby's
// Capistrano — the remote-server automation / deployment framework. It
// reproduces Capistrano's configuration variable store (set / fetch / ask with
// lazily-evaluated, memoized callable values), the server / role registry and
// host filtering (role, server, roles(:web), release_roles, primary), the task
// DSL built on a Rake-style task graph (task / namespace / desc / before /
// after / invoke / invoke!, with circular-dependency detection), and the
// SSHKit-style execution context (on(hosts) { execute / test / capture /
// upload! / download! }) with its faithful nonzero-exit / boolean / stdout
// semantics.
//
// What it is — and isn't. The *pure-compute* half of Capistrano — the variable
// store and its lazy/memoized values, the server/role model and filtering, the
// task graph and its invoke-once / circular / before-after semantics, the deploy
// flow wiring, and the execute/test/capture semantics — is deterministic and
// needs no interpreter, so it lives here as plain Go. The *effectful* half — the
// actual SSH command execution and file transfer — is an injectable Backend
// interface: the default is a real SSH backend over golang.org/x/crypto/ssh,
// but tests inject an in-process FakeBackend that records commands and returns
// scripted output, so the whole suite is deterministic and network-free. A
// future rbgo binding wraps this Go API as `require "capistrano"`; it does not
// import rbgo.
//
// The task graph mirrors Rake (Capistrano is built on Rake); the standalone
// pure-Go Rake port is github.com/go-ruby-rake/rake. Capistrano's before/after
// (prepend-prerequisite / enhance-with-post-invoke) and invoke! semantics differ
// enough that this package carries a small purpose-built graph rather than
// depending on that module.
package capistrano

import "sort"

// Callable is a lazily-evaluated configuration value — the port of a Proc
// stored in the Capistrano variable table. When fetch encounters one it calls
// it and memoizes the result back into the table (Configuration#fetch's
// `while callable? … set(key, value.call)` loop), so the body runs at most once.
type Callable func() any

// Configuration is Capistrano's variable store — the port of the variable half
// of Capistrano::Configuration. It maps symbols (as strings) to values, where a
// value may be a plain any or a Callable that is resolved and memoized on first
// fetch.
type Configuration struct {
	values map[string]any
}

// NewConfiguration returns an empty variable store.
func NewConfiguration() *Configuration {
	return &Configuration{values: map[string]any{}}
}

// Set binds key to value (Capistrano's `set`). value may be a Callable for a
// lazily-evaluated variable. Set returns value, mirroring MRI's `set`.
func (c *Configuration) Set(key string, value any) any {
	c.values[key] = value
	return value
}

// IsSet reports whether key has been assigned (Configuration#key? / `set?`).
func (c *Configuration) IsSet(key string) bool {
	_, ok := c.values[key]
	return ok
}

// Delete removes key, returning its previous value (Configuration#delete).
func (c *Configuration) Delete(key string) any {
	v := c.values[key]
	delete(c.values, key)
	return v
}

// Keys returns every assigned variable name, sorted (Configuration#keys).
func (c *Configuration) Keys() []string {
	out := make([]string, 0, len(c.values))
	for k := range c.values {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Fetch resolves key (Capistrano's `fetch`). When key is unset the optional
// default is used (a Callable default is honoured, as MRI's block default is).
// Any Callable encountered — stored value or default — is invoked and its result
// memoized back under key, so the body runs at most once and later fetches are
// cheap. A missing key with no default yields nil.
func (c *Configuration) Fetch(key string, def ...any) any {
	value, present := c.values[key]
	if !present {
		if len(def) == 0 {
			return nil
		}
		value = def[0]
	}
	for {
		cb, ok := value.(Callable)
		if !ok {
			return value
		}
		value = cb()
		c.values[key] = value
	}
}
