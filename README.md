<p align="center"><img src="https://go-ruby-capistrano.github.io/logo.png" alt="go-ruby-capistrano/capistrano" width="720"></p>

# capistrano — go-ruby-capistrano

[![Docs](https://img.shields.io/badge/docs-mkdocs--material-DC2626)](https://go-ruby-capistrano.github.io/docs/)
[![License](https://img.shields.io/badge/license-BSD--3--Clause-blue)](LICENSE)
[![Go](https://img.shields.io/badge/go-1.26.4%2B-00ADD8)](https://go.dev/dl/)
[![Coverage](https://img.shields.io/badge/coverage-100%25-1a7f37)](#tests--coverage)

**A pure-Go (no cgo) port of the core of Ruby's
[Capistrano](https://github.com/capistrano/capistrano)** — the remote-server
automation / deployment framework. It reproduces Capistrano's configuration
variable store (`set` / `fetch` / `ask` with lazily-evaluated, memoized callable
values), the server / role registry and host filtering (`role`, `server`,
`roles(:web)`, `release_roles`, `primary`), the task DSL built on a Rake-style
task graph (`task` / `namespace` / `desc` / `before` / `after` / `invoke` /
`invoke!`, with circular-dependency detection), and the SSHKit-style execution
context (`on(hosts) { execute / test / capture / upload! / download! }`) with its
faithful non-zero-exit / boolean / stdout semantics — **without any Ruby
runtime**.

It is the Capistrano backend for
[go-embedded-ruby](https://github.com/go-embedded-ruby/ruby) (a future rbgo
binding wraps this Go API as `require "capistrano"`), but is a **standalone,
reusable** module — a sibling of
[go-ruby-rake](https://github.com/go-ruby-rake/rake) and
[go-ruby-thor](https://github.com/go-ruby-thor/thor).

> **What it is — and isn't.** The *pure-compute* half of Capistrano — the
> variable store and its lazy/memoized values, the server/role model and
> filtering, the task graph and its invoke-once / circular / before-after
> semantics, the deploy flow wiring, and the `execute` / `test` / `capture`
> semantics — is deterministic and needs **no interpreter**, so it lives here as
> plain Go. The *effectful* half — the actual SSH command execution and file
> transfer — is an **injectable `Backend`**: the default is a real SSH backend
> over `golang.org/x/crypto/ssh`, but tests inject an in-process `FakeBackend`
> that records commands and returns scripted output, so the whole suite is
> deterministic and network-free.

Capistrano is built on Rake; the standalone pure-Go Rake port is
[go-ruby-rake/rake](https://github.com/go-ruby-rake/rake). Capistrano's
`before` / `after` (prepend-prerequisite / enhance-with-post-invoke) and
`invoke!` semantics differ enough that this package carries a small
purpose-built task graph rather than depending on that module.

## Install

```sh
go get github.com/go-ruby-capistrano/capistrano
```

## Usage

```go
package main

import (
	"fmt"

	cap "github.com/go-ruby-capistrano/capistrano"
)

func main() {
	app := cap.NewApplication()

	// config (set / fetch, with a lazy memoized value)
	app.Set("application", "shop")
	app.Set("repo_url", "git@github.com:acme/shop.git")
	app.Set("deploy_to", cap.Callable(func() any { return "/srv/" + app.Fetch("application").(string) }))

	// servers & roles
	app.Role("web", []string{"web1.example.com", "web2.example.com"}, nil)
	app.AddServer("db1.example.com", []string{"db"}, map[string]any{"primary": true})

	// a custom task using the execution seam
	app.Namespace("shop", func() {
		app.Desc("Print the release path on every web host")
		app.Task("where", nil, func() error {
			return app.On(app.Roles("web"), func(s *cap.Session) error {
				out, err := s.Capture("readlink", app.Fetch("deploy_to").(string)+"/current")
				if err != nil {
					return err
				}
				fmt.Println(s.Host(), "->", out)
				return nil
			})
		})
	})

	// hooks + the built-in deploy flow
	_ = app.After("deploy:published", "shop:where")

	// In production this runs over real SSH. In a test, inject a FakeBackend:
	//   fb := cap.NewFakeBackend(); app.SetBackend(fb)
	_ = app.Invoke("deploy")
}
```

## API (what a future rbgo binding wraps)

```go
// Application — the DSL facade (the global `env` + the Rake application)
func NewApplication() *Application
func (app *Application) Set(key string, value any) any            // set
func (app *Application) Fetch(key string, def ...any) any         // fetch (memoizes callables)
func (app *Application) IsSet(key string) bool                    // set?
func (app *Application) Ask(key, prompt string, def any)          // ask
func (app *Application) Role(role string, hosts []string, props map[string]any)
func (app *Application) AddServer(host string, roles []string, props map[string]any) *Server
func (app *Application) Roles(names ...string) []*Server          // roles(:web)
func (app *Application) ReleaseRoles(names ...string) []*Server   // release_roles
func (app *Application) Primary(role string) *Server              // primary
func (app *Application) Desc(text string)                         // desc
func (app *Application) Task(name string, deps []string, body TaskBody) *Task
func (app *Application) Namespace(name string, body func())       // namespace
func (app *Application) Before(task, prerequisite string) error   // before
func (app *Application) After(task, post string) error            // after
func (app *Application) Invoke(name string) error                 // invoke
func (app *Application) InvokeBang(name string) error             // invoke!
func (app *Application) On(hosts []*Server, block OnBlock) error  // on(hosts) { … }
func (app *Application) Stage(name string, body func())           // stage config
func (app *Application) LoadStage(name string) error              // production / staging

// Session — the SSHKit context yielded to on-blocks
func (s *Session) Execute(command string, args ...string) error       // raises on non-zero
func (s *Session) Test(command string, args ...string) (bool, error)  // boolean
func (s *Session) Capture(command string, args ...string) (string, error) // stripped stdout
func (s *Session) Upload(local, remote string) error                  // upload!
func (s *Session) Download(remote, local string) error                // download!
func (s *Session) Host() *Server

// Backend — the injectable execution seam (default: NewSSHBackend; tests: FakeBackend)
type Backend interface {
	Run(host *Server, command string) (CommandResult, error)
	Upload(host *Server, local, remote string) error
	Download(host *Server, remote, local string) error
}
func (app *Application) SetBackend(b Backend)

// Injectable seams: SetInput (ask), SetReleaseTimestamp, SetSCM.
// Config / Server / Servers / TaskManager are exposed for direct use.
```

The default deploy flow (`deploy` → `deploy:starting` → `deploy:updating` →
`deploy:publishing` → `deploy:finishing`, with `deploy:check` and
`deploy:log_revision`) is registered by `NewApplication`, drives the `Git` SCM
strategy, and issues every command through the injectable `Backend`.

## Deferred (out of scope for this core)

This is a faithful *core*, not a drop-in production deploy tool. Explicitly
deferred:

- **The plugin ecosystem** — `capistrano-rails`, `capistrano-bundler`,
  `capistrano-rbenv`, rvm/passenger/puma/sidekiq tasks, etc. The task DSL and
  hooks are all present, so such plugins are ordinary task definitions layered on
  top.
- **Real file transfer over the default SSH backend** — `upload!` / `download!`
  return a "deferred" error on the built-in SSH backend (they need an SFTP/SCP
  sub-channel). The `Backend` seam is complete, so a host injects a transferring
  backend; the `FakeBackend` records transfers for tests.
- **SSH connection multiplexing / parallel `on`** — `on(hosts)` runs the block
  sequentially per host. SSHKit's `in: :parallel`/`:groups`, connection pooling,
  and ControlMaster reuse are not implemented.
- **Full mirror-clone / archive release strategy & symlinked shared paths** —
  `deploy:updating` performs a compact `git clone` rather than Capistrano's
  cached-mirror + `git archive` + `linked_files` / `linked_dirs` machinery.
- **`ask` echo/redaction options, `run_locally`, `Rake`-style file tasks and
  rules, the CLI (`cap`), and Airbrussh formatting.**

## Tests & coverage

The suite is Ruby-free and network-free. It drives the full DSL and a sample
deploy flow against the in-process `FakeBackend`, and covers the real SSH
backend with an **in-process loopback ssh server** (over `x/crypto/ssh`), so no
external host is needed. Coverage is held at **100% of statements**, including
every error branch (command non-zero exit, missing task, no matching hosts,
capture of a failing command, transport failure).

```sh
go test -covermode=count -coverprofile=cover.out ./...
go tool cover -func=cover.out | tail -1   # 100.0%
```

CGO-free, `gofmt` + `go vet` clean, and green across the six 64-bit Go targets
(amd64, arm64, riscv64, loong64, ppc64le, s390x), the two WebAssembly targets
(`js/wasm`, `wasip1/wasm`), and three OSes (Linux, macOS, Windows).

## License

BSD-3-Clause — see [LICENSE](LICENSE). Copyright the go-ruby-capistrano/capistrano authors.
