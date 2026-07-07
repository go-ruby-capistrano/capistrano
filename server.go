// Copyright (c) the go-ruby-capistrano/capistrano authors
//
// SPDX-License-Identifier: BSD-3-Clause

package capistrano

import (
	"sort"
	"strconv"
	"strings"
)

// Server is a single deployment target — the port of Capistrano::Configuration::
// Server (an SSHKit::Host). It carries the connection triple (user, host, port),
// the set of roles it plays, and an arbitrary property bag (Capistrano's
// per-server variables such as primary, no_release, ssh_options).
type Server struct {
	User  string
	Host  string
	Port  int
	roles map[string]bool
	props map[string]any
}

// NewServer parses an SSHKit host string ("[user@]host[:port]") into a Server —
// the port of Server.new / SSHKit::Host#initialize. A missing user or port is
// left zero-valued (the host's defaults apply).
func NewServer(hostString string) *Server {
	s := &Server{roles: map[string]bool{}, props: map[string]any{}}
	rest := hostString
	if at := strings.LastIndex(rest, "@"); at >= 0 {
		s.User = rest[:at]
		rest = rest[at+1:]
	}
	if colon := strings.LastIndex(rest, ":"); colon >= 0 {
		if p, err := strconv.Atoi(rest[colon+1:]); err == nil {
			s.Port = p
			rest = rest[:colon]
		}
	}
	s.Host = rest
	return s
}

// AddRole records that the server plays each of roles (Server#add_role),
// returning the server for chaining. The special role :all is never stored — it
// is a wildcard the registry expands.
func (s *Server) AddRole(roles ...string) *Server {
	for _, r := range roles {
		if r != "all" {
			s.roles[r] = true
		}
	}
	return s
}

// HasRole reports whether the server plays role (Server#roles.include?). The
// wildcard :all matches every server.
func (s *Server) HasRole(role string) bool {
	if role == "all" {
		return true
	}
	return s.roles[role]
}

// Roles returns the server's roles, sorted (Server#roles as a stable slice).
func (s *Server) Roles() []string {
	out := make([]string, 0, len(s.roles))
	for r := range s.roles {
		out = append(out, r)
	}
	sort.Strings(out)
	return out
}

// Set assigns a per-server property (Server#set / with), returning the server.
func (s *Server) Set(key string, value any) *Server {
	s.props[key] = value
	return s
}

// Fetch reads a per-server property, or nil when unset (Server#fetch).
func (s *Server) Fetch(key string) any { return s.props[key] }

// HasProperty reports whether a per-server property has been set.
func (s *Server) HasProperty(key string) bool {
	_, ok := s.props[key]
	return ok
}

// IsPrimary reports whether the server was flagged `primary: true` (Server#
// primary?). The registry prefers such a server when resolving primary(role).
func (s *Server) IsPrimary() bool {
	v, ok := s.props["primary"]
	if !ok {
		return false
	}
	b, ok := v.(bool)
	return ok && b
}

// NoRelease reports whether the server opted out of releases via
// `no_release: true` (Server#no_release?), excluding it from release_roles.
func (s *Server) NoRelease() bool {
	v, ok := s.props["no_release"]
	if !ok {
		return false
	}
	b, ok := v.(bool)
	return ok && b
}

// HostPort returns the "host:port" dial target, defaulting the port to 22 when
// unset (the SSH default).
func (s *Server) HostPort() string {
	port := s.Port
	if port == 0 {
		port = 22
	}
	return s.Host + ":" + strconv.Itoa(port)
}

// String renders the canonical "[user@]host[:port]" form (Server#to_s).
func (s *Server) String() string {
	var b strings.Builder
	if s.User != "" {
		b.WriteString(s.User)
		b.WriteByte('@')
	}
	b.WriteString(s.Host)
	if s.Port != 0 {
		b.WriteByte(':')
		b.WriteString(strconv.Itoa(s.Port))
	}
	return b.String()
}

// key is the identity a Servers registry de-duplicates on (user@host:port) —
// SSHKit merges two host declarations that resolve to the same endpoint.
func (s *Server) key() string { return s.String() }
