// Copyright (c) the go-ruby-capistrano/capistrano authors
//
// SPDX-License-Identifier: BSD-3-Clause

package capistrano

import "golang.org/x/crypto/ssh"

// OnBlock is the body of an on(hosts) { … } context, run once per matching host
// with a Session bound to that host — the port of the SSHKit block. A returned
// error aborts the run and is propagated to the caller.
type OnBlock func(s *Session) error

// Application is the Capistrano DSL facade — the port of the global `env`
// (Capistrano::Configuration.env) plus the Rake application. It aggregates the
// variable store, the server/role registry, the task graph, the injectable
// command Backend, the interactive-input seam (for ask), and the registered
// stages, and exposes the Ruby-named DSL methods (set/fetch/ask, role/server/
// roles/…, task/namespace/before/after/invoke, on) a host binds.
type Application struct {
	config  *Configuration
	servers *Servers
	tasks   *TaskManager
	backend Backend
	input   func(prompt string) string
	stages  map[string]func()
	// releaseTimestamp names each release directory; a seam so tests get
	// deterministic paths (Capistrano uses a UTC timestamp).
	releaseTimestamp func() string
	// scm is the source-control strategy the deploy flow issues commands
	// through (default Git).
	scm SCM
}

// NewApplication returns a fully-wired DSL facade: an empty variable store,
// server registry and task graph, the default real-SSH Backend (host-key checks
// disabled, no auth — a deploy overrides it via set / a custom ssh.ClientConfig),
// a non-interactive input seam (ask falls back to its default), a fixed release
// timestamp, and the Git SCM strategy. The default deploy flow is registered.
func NewApplication() *Application {
	app := &Application{
		config:           NewConfiguration(),
		servers:          NewServers(),
		tasks:            NewTaskManager(),
		backend:          NewSSHBackend(&ssh.ClientConfig{HostKeyCallback: ssh.InsecureIgnoreHostKey()}),
		input:            func(string) string { return "" },
		stages:           map[string]func(){},
		releaseTimestamp: func() string { return "20260707000000" },
		scm:              Git{},
	}
	app.installDeployFlow()
	return app
}

// SetBackend injects a Backend (a FakeBackend in tests, a custom transferring
// backend in production) — the seam the whole execution layer hangs on.
func (app *Application) SetBackend(b Backend) { app.backend = b }

// Backend returns the installed command Backend.
func (app *Application) Backend() Backend { return app.backend }

// SetInput injects the interactive-input seam ask reads answers from (rbgo wires
// it to $stdin; tests wire a scripted function).
func (app *Application) SetInput(fn func(prompt string) string) { app.input = fn }

// SetReleaseTimestamp injects the release-directory naming seam.
func (app *Application) SetReleaseTimestamp(fn func() string) { app.releaseTimestamp = fn }

// SetSCM injects the source-control strategy the deploy flow uses.
func (app *Application) SetSCM(s SCM) { app.scm = s }

// Config exposes the underlying variable store.
func (app *Application) Config() *Configuration { return app.config }

// Servers exposes the underlying server/role registry.
func (app *Application) Servers() *Servers { return app.servers }

// Tasks exposes the underlying task graph.
func (app *Application) Tasks() *TaskManager { return app.tasks }

// --- variable DSL (set / fetch / ask) ---------------------------------------

// Set binds key (the `set` DSL); value may be a Callable for a lazy variable.
func (app *Application) Set(key string, value any) any { return app.config.Set(key, value) }

// Fetch resolves key with an optional default (the `fetch` DSL), memoizing any
// callable it evaluates.
func (app *Application) Fetch(key string, def ...any) any { return app.config.Fetch(key, def...) }

// IsSet reports whether key is assigned (the `set?` DSL).
func (app *Application) IsSet(key string) bool { return app.config.IsSet(key) }

// Ask registers key as a question (the `ask` DSL): on first fetch it prompts via
// the input seam, falling back to def when the answer is blank, and memoizes.
func (app *Application) Ask(key, prompt string, def any) {
	app.config.Set(key, Callable(func() any {
		answer := app.input(prompt)
		if answer == "" {
			return def
		}
		return answer
	}))
}

// --- server / role DSL ------------------------------------------------------

// Role assigns role to hosts (the `role name, hosts` DSL).
func (app *Application) Role(role string, hosts []string, props map[string]any) {
	app.servers.AddRole(role, hosts, props)
}

// AddServer registers a server with roles and properties (the `server` DSL).
func (app *Application) AddServer(host string, roles []string, props map[string]any) *Server {
	return app.servers.Add(host, roles, props)
}

// Roles returns the servers in any of the named roles (the `roles` DSL).
func (app *Application) Roles(names ...string) []*Server { return app.servers.Roles(names...) }

// ReleaseRoles returns the release-eligible servers in the roles (the
// `release_roles` DSL).
func (app *Application) ReleaseRoles(names ...string) []*Server {
	return app.servers.ReleaseRoles(names...)
}

// Primary returns the primary server for role (the `primary` DSL).
func (app *Application) Primary(role string) *Server { return app.servers.Primary(role) }

// --- task DSL ---------------------------------------------------------------

// Desc sets the next task's description (the `desc` DSL).
func (app *Application) Desc(text string) { app.tasks.Desc(text) }

// Task defines/enhances a task (the `task` DSL).
func (app *Application) Task(name string, deps []string, body TaskBody) *Task {
	return app.tasks.Define(name, deps, body)
}

// Namespace scopes body's task definitions under name (the `namespace` DSL).
func (app *Application) Namespace(name string, body func()) { app.tasks.Namespace(name, body) }

// Before wires prerequisite to run before task (the `before` DSL).
func (app *Application) Before(task, prerequisite string) error {
	return app.tasks.Before(task, prerequisite)
}

// After wires post to run after task (the `after` DSL).
func (app *Application) After(task, post string) error { return app.tasks.After(task, post) }

// Invoke runs a task once (the `invoke` DSL).
func (app *Application) Invoke(name string) error { return app.tasks.Invoke(name) }

// InvokeBang re-runs a task even if already invoked (the `invoke!` DSL).
func (app *Application) InvokeBang(name string) error { return app.tasks.InvokeBang(name) }

// --- execution DSL (on) -----------------------------------------------------

// On runs block once per host with a Session bound to it (the `on(hosts) { … }`
// DSL). With no hosts it raises NoMatchingServersError; the first block error
// aborts and is returned.
func (app *Application) On(hosts []*Server, block OnBlock) error {
	if len(hosts) == 0 {
		return &NoMatchingServersError{}
	}
	for _, h := range hosts {
		if err := block(&Session{backend: app.backend, host: h}); err != nil {
			return err
		}
	}
	return nil
}

// --- stages -----------------------------------------------------------------

// Stage registers a named stage body (a production/staging config block); Load
// runs it (the `capistrano/setup` stage mechanism).
func (app *Application) Stage(name string, body func()) { app.stages[name] = body }

// LoadStage runs the named stage's body, defining its servers, roles and
// variables. It raises a Capistrano error when no such stage is registered.
func (app *Application) LoadStage(name string) error {
	body, ok := app.stages[name]
	if !ok {
		return &Error{Message: "stage not found: " + name}
	}
	app.Set("stage", name)
	if body != nil {
		body()
	}
	return nil
}
