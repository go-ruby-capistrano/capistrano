// Copyright (c) the go-ruby-capistrano/capistrano authors
//
// SPDX-License-Identifier: BSD-3-Clause

package capistrano

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// --- Configuration ----------------------------------------------------------

func TestConfigurationSetFetchDeleteKeys(t *testing.T) {
	c := NewConfiguration()
	if got := c.Set("a", 1); got != 1 {
		t.Fatalf("Set returned %v, want 1", got)
	}
	if !c.IsSet("a") {
		t.Fatal("IsSet(a) = false")
	}
	if c.IsSet("missing") {
		t.Fatal("IsSet(missing) = true")
	}
	c.Set("b", 2)
	if got := c.Keys(); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("Keys = %v", got)
	}
	if got := c.Delete("a"); got != 1 {
		t.Fatalf("Delete(a) = %v, want 1", got)
	}
	if c.IsSet("a") {
		t.Fatal("a still set after Delete")
	}
}

func TestConfigurationFetch(t *testing.T) {
	c := NewConfiguration()
	// present
	c.Set("host", "example.com")
	if got := c.Fetch("host"); got != "example.com" {
		t.Fatalf("Fetch(host) = %v", got)
	}
	// absent, no default
	if got := c.Fetch("nope"); got != nil {
		t.Fatalf("Fetch(nope) = %v, want nil", got)
	}
	// absent, plain default (not memoized)
	if got := c.Fetch("nope", "def"); got != "def" {
		t.Fatalf("Fetch(nope, def) = %v", got)
	}
	if c.IsSet("nope") {
		t.Fatal("plain default should not memoize")
	}
	// callable value, memoized after first fetch
	calls := 0
	c.Set("lazy", Callable(func() any { calls++; return 42 }))
	if got := c.Fetch("lazy"); got != 42 {
		t.Fatalf("Fetch(lazy) = %v", got)
	}
	if got := c.Fetch("lazy"); got != 42 || calls != 1 {
		t.Fatalf("Fetch(lazy) second = %v calls=%d, want 42 calls=1", got, calls)
	}
	// callable default is honoured and memoized under the key
	if got := c.Fetch("lazydef", Callable(func() any { return 7 })); got != 7 {
		t.Fatalf("Fetch(lazydef) = %v", got)
	}
	if !c.IsSet("lazydef") {
		t.Fatal("callable default should memoize")
	}
}

// --- Server -----------------------------------------------------------------

func TestNewServerParsing(t *testing.T) {
	cases := []struct {
		in         string
		user, host string
		port       int
	}{
		{"example.com", "", "example.com", 0},
		{"deploy@example.com", "deploy", "example.com", 0},
		{"example.com:2222", "", "example.com", 2222},
		{"deploy@example.com:2222", "deploy", "example.com", 2222},
		{"host:notaport", "", "host:notaport", 0}, // non-numeric port stays in host
	}
	for _, tc := range cases {
		s := NewServer(tc.in)
		if s.User != tc.user || s.Host != tc.host || s.Port != tc.port {
			t.Fatalf("NewServer(%q) = {%q %q %d}, want {%q %q %d}",
				tc.in, s.User, s.Host, s.Port, tc.user, tc.host, tc.port)
		}
	}
}

func TestServerRolesPropertiesString(t *testing.T) {
	s := NewServer("deploy@web1:22")
	s.AddRole("web", "app", "all") // "all" is a wildcard, never stored
	if !s.HasRole("web") || !s.HasRole("all") || s.HasRole("db") {
		t.Fatal("HasRole wrong")
	}
	if got := s.Roles(); !reflect.DeepEqual(got, []string{"app", "web"}) {
		t.Fatalf("Roles = %v", got)
	}
	// properties
	s.Set("primary", true).Set("weight", 5)
	if !s.HasProperty("primary") || s.HasProperty("nope") {
		t.Fatal("HasProperty wrong")
	}
	if s.Fetch("weight") != 5 {
		t.Fatal("Fetch(weight) wrong")
	}
	if !s.IsPrimary() {
		t.Fatal("IsPrimary should be true")
	}
	if s.NoRelease() {
		t.Fatal("NoRelease should be false when unset")
	}
	if got, want := s.HostPort(), "web1:22"; got != want {
		t.Fatalf("HostPort = %q", got)
	}
	if got, want := s.String(), "deploy@web1:22"; got != want {
		t.Fatalf("String = %q", got)
	}
}

func TestServerDefaultsAndNonBoolFlags(t *testing.T) {
	s := NewServer("host") // no user, no port
	if got := s.HostPort(); got != "host:22" {
		t.Fatalf("HostPort default = %q", got)
	}
	if got := s.String(); got != "host" {
		t.Fatalf("String = %q", got)
	}
	// primary/no_release set to non-bool values → treated as false
	s.Set("primary", "yes").Set("no_release", 1)
	if s.IsPrimary() {
		t.Fatal("non-bool primary should be false")
	}
	if s.NoRelease() {
		t.Fatal("non-bool no_release should be false")
	}
	// unset primary → false
	if NewServer("x").IsPrimary() {
		t.Fatal("unset primary should be false")
	}
	// no_release true honoured
	rel := NewServer("y")
	rel.Set("no_release", true)
	if !rel.NoRelease() {
		t.Fatal("no_release true should be honoured")
	}
}

// --- Servers registry -------------------------------------------------------

func TestServersRegistry(t *testing.T) {
	reg := NewServers()
	reg.AddRole("web", []string{"web1", "web2"}, map[string]any{"port": 80})
	// re-declaring web1 as app merges roles into the same server
	reg.Add("web1", []string{"app"}, map[string]any{"primary": true})
	if got := len(reg.All()); got != 2 {
		t.Fatalf("All len = %d, want 2 (web1 merged)", got)
	}
	web := reg.Roles("web")
	if len(web) != 2 {
		t.Fatalf("Roles(web) = %d", len(web))
	}
	app := reg.Roles("app")
	if len(app) != 1 || app[0].Host != "web1" {
		t.Fatalf("Roles(app) = %v", app)
	}
	// wildcard :all
	if len(reg.Roles("all")) != 2 {
		t.Fatal("Roles(all) should be everyone")
	}
	// primary: web1 flagged primary
	if p := reg.Primary("web"); p == nil || p.Host != "web1" {
		t.Fatalf("Primary(web) = %v", p)
	}
	// role names deduped + sorted
	if got := reg.RoleNames(); !reflect.DeepEqual(got, []string{"app", "web"}) {
		t.Fatalf("RoleNames = %v", got)
	}
}

func TestServersReleaseRolesAndPrimaryFallback(t *testing.T) {
	reg := NewServers()
	reg.Add("w1", []string{"web"}, nil)
	reg.Add("w2", []string{"web"}, map[string]any{"no_release": true})
	if got := len(reg.Roles("web")); got != 2 {
		t.Fatalf("Roles(web) = %d", got)
	}
	rr := reg.ReleaseRoles("web")
	if len(rr) != 1 || rr[0].Host != "w1" {
		t.Fatalf("ReleaseRoles(web) = %v", rr)
	}
	// primary fallback: first in role (none flagged primary)
	if p := reg.Primary("web"); p == nil || p.Host != "w1" {
		t.Fatalf("Primary fallback = %v", p)
	}
	// primary of an empty role is nil
	if p := reg.Primary("db"); p != nil {
		t.Fatalf("Primary(db) = %v, want nil", p)
	}
}

// --- TaskManager ------------------------------------------------------------

func TestTaskManagerInvokeOrderAndOnce(t *testing.T) {
	m := NewTaskManager()
	var order []string
	rec := func(name string) TaskBody { return func() error { order = append(order, name); return nil } }

	m.Define("a", nil, rec("a"))
	m.Define("b", []string{"a"}, rec("b"))
	m.Define("top", []string{"b", "a"}, rec("top"))
	if err := m.Invoke("top"); err != nil {
		t.Fatalf("Invoke(top) err = %v", err)
	}
	if !reflect.DeepEqual(order, []string{"a", "b", "top"}) {
		t.Fatalf("order = %v", order)
	}
	// invoke again: already-invoked guard → no re-run
	order = nil
	if err := m.Invoke("top"); err != nil {
		t.Fatal(err)
	}
	if len(order) != 0 {
		t.Fatalf("second invoke re-ran: %v", order)
	}
	// invoke! re-enables and re-runs top only
	order = nil
	if err := m.InvokeBang("top"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(order, []string{"top"}) {
		t.Fatalf("invoke! order = %v", order)
	}
}

func TestTaskManagerDescEnhanceAndTasks(t *testing.T) {
	m := NewTaskManager()
	m.Desc("do a")
	ta := m.Define("a", []string{"x"}, nil)
	if ta.Description() != "do a" {
		t.Fatalf("desc = %q", ta.Description())
	}
	// enhance the same task: union prerequisites (dedup x), append body
	ran := false
	m.Define("a", []string{"x", "y"}, func() error { ran = true; return nil })
	if got := ta.Prerequisites(); !reflect.DeepEqual(got, []string{"x", "y"}) {
		t.Fatalf("prereqs = %v", got)
	}
	m.Define("x", nil, nil)
	m.Define("y", nil, nil)
	if err := m.Invoke("a"); err != nil {
		t.Fatal(err)
	}
	if !ran {
		t.Fatal("enhanced body did not run")
	}
	// Tasks sorted, Lookup
	names := []string{}
	for _, tk := range m.Tasks() {
		names = append(names, tk.Name())
	}
	if !reflect.DeepEqual(names, []string{"a", "x", "y"}) {
		t.Fatalf("Tasks = %v", names)
	}
	if _, ok := m.Lookup("nope"); ok {
		t.Fatal("Lookup(nope) ok")
	}
}

func TestTaskManagerNamespaceScoping(t *testing.T) {
	m := NewTaskManager()
	var order []string
	m.Namespace("deploy", func() {
		m.Define("a", nil, func() error { order = append(order, "a"); return nil })
		// bare "a" resolves to the sibling deploy:a (scoped exists)
		m.Define("b", []string{"a"}, func() error { order = append(order, "b"); return nil })
		// bare "ghost" has no sibling → stays a top-level reference
		m.Define("c", []string{"ghost"}, nil)
	})
	if _, ok := m.Lookup("deploy:b"); !ok {
		t.Fatal("deploy:b not defined")
	}
	if err := m.Invoke("deploy:b"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(order, []string{"a", "b"}) {
		t.Fatalf("order = %v", order)
	}
	// nil-body namespace is a no-op push/pop
	m.Namespace("empty", nil)
	// invoking deploy:c fails: ghost prerequisite is unresolved (top-level)
	if err := m.Invoke("deploy:c"); err == nil {
		t.Fatal("expected error for unresolved ghost prerequisite")
	}
}

func TestTaskManagerBeforeAfter(t *testing.T) {
	m := NewTaskManager()
	var order []string
	rec := func(n string) TaskBody { return func() error { order = append(order, n); return nil } }
	m.Define("main", nil, rec("main"))
	m.Define("pre1", nil, rec("pre1"))
	m.Define("pre2", nil, rec("pre2"))
	m.Define("post", nil, rec("post"))
	// two befores: prepended, so pre2 (declared last) runs first
	if err := m.Before("main", "pre1"); err != nil {
		t.Fatal(err)
	}
	if err := m.Before("main", "pre2"); err != nil {
		t.Fatal(err)
	}
	if err := m.After("main", "post"); err != nil {
		t.Fatal(err)
	}
	if err := m.Invoke("main"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(order, []string{"pre2", "pre1", "main", "post"}) {
		t.Fatalf("order = %v", order)
	}
	// before/after on unknown task → TaskNotFoundError
	if err := m.Before("nope", "x"); err == nil {
		t.Fatal("Before(nope) should error")
	}
	if err := m.After("nope", "x"); err == nil {
		t.Fatal("After(nope) should error")
	}
}

func TestTaskManagerErrorsAndCircular(t *testing.T) {
	m := NewTaskManager()
	// invoke unknown
	var tnf *TaskNotFoundError
	if err := m.Invoke("ghost"); !errors.As(err, &tnf) {
		t.Fatalf("Invoke(ghost) = %v, want TaskNotFoundError", err)
	}
	// invoke! of unknown also errors
	if err := m.InvokeBang("ghost"); err == nil {
		t.Fatal("InvokeBang(ghost) should error")
	}
	// action failure is remembered and re-raised on a later invoke
	boom := errors.New("boom")
	m.Define("fail", nil, func() error { return boom })
	if err := m.Invoke("fail"); err != boom {
		t.Fatalf("Invoke(fail) = %v", err)
	}
	if err := m.Invoke("fail"); err != boom {
		t.Fatalf("re-invoke(fail) = %v (remembered error)", err)
	}
	// prerequisite failure propagates
	m.Define("needsfail", []string{"fail"}, func() error { return nil })
	failTask, _ := m.Lookup("fail")
	failTask.Reenable()
	if err := m.Invoke("needsfail"); err != boom {
		t.Fatalf("Invoke(needsfail) = %v", err)
	}
	// after-hook failure propagates
	m2 := NewTaskManager()
	m2.Define("t", nil, func() error { return nil })
	m2.Define("afterboom", nil, func() error { return boom })
	_ = m2.After("t", "afterboom")
	if err := m2.Invoke("t"); err != boom {
		t.Fatalf("after-hook failure = %v", err)
	}
	// circular dependency detected
	m3 := NewTaskManager()
	m3.Define("x", []string{"y"}, nil)
	m3.Define("y", []string{"x"}, nil)
	err := m3.Invoke("x")
	if err == nil || !strings.Contains(err.Error(), "Circular dependency detected") {
		t.Fatalf("circular = %v", err)
	}
}

func TestTaskInvokeSuccessGuardNilError(t *testing.T) {
	m := NewTaskManager()
	ran := 0
	m.Define("ok", nil, func() error { ran++; return nil })
	if err := m.Invoke("ok"); err != nil {
		t.Fatal(err)
	}
	// second invoke hits the already-invoked guard with a nil remembered error
	if err := m.Invoke("ok"); err != nil {
		t.Fatal(err)
	}
	if ran != 1 {
		t.Fatalf("ran = %d, want 1", ran)
	}
}
