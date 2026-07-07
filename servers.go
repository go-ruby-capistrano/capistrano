// Copyright (c) the go-ruby-capistrano/capistrano authors
//
// SPDX-License-Identifier: BSD-3-Clause

package capistrano

import "sort"

// Servers is the server/role registry — the port of Capistrano::Configuration::
// Servers. It keeps an ordered, de-duplicated list of Server targets (SSHKit
// merges re-declarations of the same endpoint) and answers the role/property
// filter queries the DSL exposes: roles(:web), release_roles, primary(role).
type Servers struct {
	list  []*Server
	index map[string]*Server
}

// NewServers returns an empty registry.
func NewServers() *Servers {
	return &Servers{index: map[string]*Server{}}
}

// addHost returns the registry's Server for host, creating and appending it on
// first sighting and returning the existing one otherwise (Servers#add_host's
// find-or-create), so roles and properties from repeated declarations merge.
func (s *Servers) addHost(host string) *Server {
	srv := NewServer(host)
	if existing, ok := s.index[srv.key()]; ok {
		return existing
	}
	s.index[srv.key()] = srv
	s.list = append(s.list, srv)
	return srv
}

// Add registers a server with the given roles and properties (the DSL's
// `server host, roles: [...], **props`), merging into any existing endpoint.
func (s *Servers) Add(host string, roles []string, props map[string]any) *Server {
	srv := s.addHost(host)
	srv.AddRole(roles...)
	for k, v := range props {
		srv.Set(k, v)
	}
	return srv
}

// AddRole assigns role to every host, with shared properties (the DSL's
// `role name, hosts, **props`).
func (s *Servers) AddRole(role string, hosts []string, props map[string]any) {
	for _, h := range hosts {
		srv := s.addHost(h)
		srv.AddRole(role)
		for k, v := range props {
			srv.Set(k, v)
		}
	}
}

// All returns every registered server in declaration order (roles(:all)).
func (s *Servers) All() []*Server {
	return append([]*Server(nil), s.list...)
}

// Roles returns the servers playing any of the named roles, in declaration
// order (the DSL's roles(:web, :app)). The wildcard :all selects every server.
func (s *Servers) Roles(names ...string) []*Server {
	var out []*Server
	for _, srv := range s.list {
		if serverMatchesAny(srv, names) {
			out = append(out, srv)
		}
	}
	return out
}

// ReleaseRoles is Roles minus any server flagged no_release (the DSL's
// release_roles) — the hosts that actually receive a release.
func (s *Servers) ReleaseRoles(names ...string) []*Server {
	var out []*Server
	for _, srv := range s.Roles(names...) {
		if !srv.NoRelease() {
			out = append(out, srv)
		}
	}
	return out
}

// Primary returns the primary server for role (the DSL's primary(role)): the
// first host flagged `primary: true`, else the first host in the role, else nil.
func (s *Servers) Primary(role string) *Server {
	matches := s.Roles(role)
	for _, srv := range matches {
		if srv.IsPrimary() {
			return srv
		}
	}
	if len(matches) > 0 {
		return matches[0]
	}
	return nil
}

// RoleNames returns every distinct role across the registry, sorted (the DSL's
// role_names).
func (s *Servers) RoleNames() []string {
	seen := map[string]bool{}
	var out []string
	for _, srv := range s.list {
		for _, r := range srv.Roles() {
			if !seen[r] {
				seen[r] = true
				out = append(out, r)
			}
		}
	}
	sort.Strings(out)
	return out
}

// serverMatchesAny reports whether srv plays at least one of names.
func serverMatchesAny(srv *Server, names []string) bool {
	for _, n := range names {
		if srv.HasRole(n) {
			return true
		}
	}
	return false
}
