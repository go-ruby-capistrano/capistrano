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

func TestApplicationAccessorsAndDefaults(t *testing.T) {
	app := NewApplication()
	if app.Config() == nil || app.Servers() == nil || app.Tasks() == nil {
		t.Fatal("nil sub-object")
	}
	if app.Backend() == nil {
		t.Fatal("default backend nil")
	}
	// setters
	fb := NewFakeBackend()
	app.SetBackend(fb)
	if app.Backend() != fb {
		t.Fatal("SetBackend failed")
	}
	app.SetReleaseTimestamp(func() string { return "TS" })
	app.SetSCM(Git{})
	app.SetInput(func(string) string { return "" })
}

func TestApplicationVariableDSL(t *testing.T) {
	app := NewApplication()
	app.Set("application", "shop")
	if app.Fetch("application") != "shop" {
		t.Fatal("Fetch application")
	}
	if !app.IsSet("application") || app.IsSet("nope") {
		t.Fatal("IsSet")
	}
	if app.Fetch("missing", "d") != "d" {
		t.Fatal("Fetch default")
	}
}

func TestApplicationAsk(t *testing.T) {
	app := NewApplication()
	// answered
	app.SetInput(func(prompt string) string {
		if !strings.Contains(prompt, "branch") {
			t.Fatalf("prompt = %q", prompt)
		}
		return "release"
	})
	app.Ask("branch", "branch?", "main")
	if got := app.Fetch("branch"); got != "release" {
		t.Fatalf("Ask answered = %v", got)
	}
	// blank answer falls back to default
	app.SetInput(func(string) string { return "" })
	app.Ask("tag", "tag?", "v1")
	if got := app.Fetch("tag"); got != "v1" {
		t.Fatalf("Ask default = %v", got)
	}
}

func TestApplicationServerDSL(t *testing.T) {
	app := NewApplication()
	app.Role("web", []string{"web1", "web2"}, nil)
	app.AddServer("db1", []string{"db"}, map[string]any{"primary": true})
	if got := len(app.Roles("web")); got != 2 {
		t.Fatalf("Roles(web) = %d", got)
	}
	if got := len(app.ReleaseRoles("web", "db")); got != 3 {
		t.Fatalf("ReleaseRoles = %d", got)
	}
	if p := app.Primary("db"); p == nil || p.Host != "db1" {
		t.Fatalf("Primary(db) = %v", p)
	}
}

func TestApplicationTaskDSL(t *testing.T) {
	app := NewApplication()
	var order []string
	app.Desc("greet")
	app.Task("greet", nil, func() error { order = append(order, "greet"); return nil })
	app.Namespace("ns", func() {
		app.Task("inner", nil, func() error { order = append(order, "inner"); return nil })
	})
	app.Task("both", []string{"greet", "ns:inner"}, nil)
	if err := app.Before("both", "greet"); err != nil {
		t.Fatal(err)
	}
	if err := app.After("both", "ns:inner"); err != nil {
		t.Fatal(err)
	}
	if err := app.Invoke("both"); err != nil {
		t.Fatal(err)
	}
	if err := app.InvokeBang("greet"); err != nil {
		t.Fatal(err)
	}
	if len(order) == 0 {
		t.Fatal("no tasks ran")
	}
}

func TestApplicationOn(t *testing.T) {
	app := NewApplication()
	fb := NewFakeBackend()
	app.SetBackend(fb)
	app.Role("web", []string{"web1", "web2"}, nil)

	// success: block runs once per host
	count := 0
	if err := app.On(app.Roles("web"), func(s *Session) error {
		count++
		return s.Execute("echo", "hi")
	}); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("block ran %d times, want 2", count)
	}
	// empty hosts → NoMatchingServersError
	var nm *NoMatchingServersError
	if err := app.On(app.Roles("ghost"), func(*Session) error { return nil }); !errors.As(err, &nm) {
		t.Fatalf("On(empty) = %v, want NoMatchingServersError", err)
	}
	// block error propagates and aborts
	boom := errors.New("boom")
	if err := app.On(app.Roles("web"), func(*Session) error { return boom }); err != boom {
		t.Fatalf("On block error = %v", err)
	}
}

func TestApplicationStages(t *testing.T) {
	app := NewApplication()
	loaded := false
	app.Stage("production", func() {
		loaded = true
		app.Role("web", []string{"prod-web"}, nil)
	})
	// staging with a nil body is valid (just sets the stage var)
	app.Stage("staging", nil)

	if err := app.LoadStage("production"); err != nil {
		t.Fatal(err)
	}
	if !loaded || app.Fetch("stage") != "production" {
		t.Fatal("production stage not applied")
	}
	if len(app.Roles("web")) != 1 {
		t.Fatal("production servers not registered")
	}
	if err := app.LoadStage("staging"); err != nil {
		t.Fatal(err)
	}
	// unknown stage errors
	if err := app.LoadStage("qa"); err == nil {
		t.Fatal("LoadStage(qa) should error")
	}
}

// --- Full deploy flow against the fake backend ------------------------------

func TestDeployFlowEndToEnd(t *testing.T) {
	app := NewApplication()
	fb := NewFakeBackend()
	app.SetBackend(fb)
	app.SetReleaseTimestamp(func() string { return "20260707120000" })
	app.Set("repo_url", "git@github.com:acme/shop.git")
	app.Set("branch", "release")
	app.Set("deploy_to", "/srv/shop")
	app.Role("web", []string{"web1"}, nil)
	// scripted revision capture for deploy:log_revision
	fb.Script("git -C /srv/shop/releases/20260707120000 rev-parse HEAD",
		CommandResult{Stdout: "abc123\n"}, nil)

	if err := app.Invoke("deploy"); err != nil {
		t.Fatalf("deploy failed: %v", err)
	}

	joined := strings.Join(fb.Commands, "\n")
	for _, want := range []string{
		"git ls-remote git@github.com:acme/shop.git", // deploy:check (before starting)
		"mkdir -p /srv/shop/releases/20260707120000", // updating
		"git clone --branch release git@github.com:acme/shop.git /srv/shop/releases/20260707120000",
		"ln -sfn /srv/shop/releases/20260707120000 /srv/shop/current", // publishing
		"cleanup-releases /srv/shop",                                  // finishing
		"git -C /srv/shop/releases/20260707120000 rev-parse HEAD",     // log_revision
		"log-revision abc123 /srv/shop/current",                       // log_revision after
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("deploy commands missing %q\n---\n%s", want, joined)
		}
	}
}

func TestDeployBranchDefaultAndFailures(t *testing.T) {
	// default branch is "main" when unset
	app := NewApplication()
	fb := NewFakeBackend()
	app.SetBackend(fb)
	app.Set("repo_url", "repo")
	app.Role("web", []string{"web1"}, nil)
	if err := app.Invoke("deploy:updating"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(fb.Commands, "\n"), "--branch main ") {
		t.Fatalf("expected default branch main, got %v", fb.Commands)
	}

	// mkdir failure aborts deploy:updating (Release's first Execute)
	app2 := NewApplication()
	fb2 := NewFakeBackend()
	app2.SetBackend(fb2)
	app2.SetReleaseTimestamp(func() string { return "TS" })
	app2.Set("repo_url", "repo")
	app2.Role("web", []string{"web1"}, nil)
	fb2.Script("mkdir -p /var/www/app/releases/TS", CommandResult{ExitStatus: 1, Stderr: "denied"}, nil)
	var ce *CommandError
	if err := app2.Invoke("deploy:updating"); !errors.As(err, &ce) {
		t.Fatalf("updating mkdir failure = %v", err)
	}

	// revision capture failure aborts deploy:log_revision
	app3 := NewApplication()
	fb3 := NewFakeBackend()
	app3.SetBackend(fb3)
	app3.SetReleaseTimestamp(func() string { return "TS" })
	app3.Set("repo_url", "repo")
	app3.Role("web", []string{"web1"}, nil)
	fb3.Script("git -C /var/www/app/releases/TS rev-parse HEAD",
		CommandResult{ExitStatus: 2, Stderr: "no repo"}, nil)
	// updating first so release_timestamp is set
	if err := app3.Invoke("deploy:updating"); err != nil {
		t.Fatal(err)
	}
	if err := app3.Invoke("deploy:log_revision"); !errors.As(err, &ce) {
		t.Fatalf("log_revision revision failure = %v", err)
	}

	// deploy:check with no servers → NoMatchingServersError
	app4 := NewApplication()
	app4.SetBackend(NewFakeBackend())
	var nm *NoMatchingServersError
	if err := app4.Invoke("deploy:check"); !errors.As(err, &nm) {
		t.Fatalf("check no servers = %v", err)
	}
}

func TestPathHelpers(t *testing.T) {
	c := NewConfiguration()
	// defaults
	if got := deployTo(c); got != "/var/www/app" {
		t.Fatalf("deployTo default = %q", got)
	}
	if got := currentPath(c); got != "/var/www/app/current" {
		t.Fatalf("currentPath = %q", got)
	}
	// explicit release_path short-circuits the computed one
	c.Set("release_path", "/custom/rel")
	if got := releasePath(c); got != "/custom/rel" {
		t.Fatalf("releasePath explicit = %q", got)
	}
	// strOr fallback for an empty string value and a non-string value
	c.Set("deploy_to", "")
	if got := deployTo(c); got != "/var/www/app" {
		t.Fatalf("empty deploy_to should fall back, got %q", got)
	}
	c.Set("branch", 123)
	if got := branch(c); got != "main" {
		t.Fatalf("non-string branch should fall back, got %q", got)
	}
	// str of nil
	if got := str(nil); got != "" {
		t.Fatalf("str(nil) = %q", got)
	}
	_ = reflect.DeepEqual
}
