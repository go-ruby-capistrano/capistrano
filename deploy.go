// Copyright (c) the go-ruby-capistrano/capistrano authors
//
// SPDX-License-Identifier: BSD-3-Clause

package capistrano

// SCM is the source-control strategy the deploy flow drives through the
// execution seam — the port of Capistrano's SCM plugins (Capistrano::SCM::Git
// and friends). Every method issues its commands through the given Session, so
// the actual git invocations flow through the injectable Backend like any other
// command.
type SCM interface {
	// Check verifies the deploy host can reach the repository (deploy:check).
	Check(s *Session, c *Configuration) error
	// Release materialises a release into c's release_path (deploy:updating).
	Release(s *Session, c *Configuration) error
	// Revision captures the deployed revision SHA (deploy:log_revision).
	Revision(s *Session, c *Configuration) (string, error)
}

// Git is the default SCM strategy — the port of Capistrano::SCM::Git. It issues
// git over the Session; it does not implement git itself.
type Git struct{}

// repoURL / branch read the repository configuration, with Capistrano's defaults.
func repoURL(c *Configuration) string { return str(c.Fetch("repo_url")) }
func branch(c *Configuration) string  { return strOr(c.Fetch("branch"), "main") }

// Check runs `git ls-remote <repo_url>` on the host, raising if the repository
// is unreachable (deploy:check's git access probe).
func (Git) Check(s *Session, c *Configuration) error {
	return s.Execute("git", "ls-remote", repoURL(c))
}

// Release clones the branch into release_path (deploy:updating). It first
// ensures the release directory exists, then clones — a compact stand-in for
// Capistrano's mirror-clone + archive strategy (see the README "Deferred" note).
func (Git) Release(s *Session, c *Configuration) error {
	if err := s.Execute("mkdir", "-p", releasePath(c)); err != nil {
		return err
	}
	return s.Execute("git", "clone", "--branch", branch(c), repoURL(c), releasePath(c))
}

// Revision captures the deployed commit SHA (`git rev-parse HEAD` in the
// release), used for the revisions.log entry.
func (Git) Revision(s *Session, c *Configuration) (string, error) {
	return s.Capture("git", "-C", releasePath(c), "rev-parse", "HEAD")
}

// --- path helpers (Capistrano's deploy_to / releases / current layout) -------

func deployTo(c *Configuration) string { return strOr(c.Fetch("deploy_to"), "/var/www/app") }
func releasePath(c *Configuration) string {
	return strOr(c.Fetch("release_path"), deployTo(c)+"/releases/"+str(c.Fetch("release_timestamp")))
}
func currentPath(c *Configuration) string { return deployTo(c) + "/current" }

// installDeployFlow registers Capistrano's default deploy task flow — the
// starting → updating → publishing → finishing sequence, the deploy:check
// precondition, and the deploy:log_revision record — all driving the SCM and the
// execution seam. It mirrors deploy.rake's structure closely enough to invoke
// end-to-end against an injected Backend.
func (app *Application) installDeployFlow() {
	c := app.config

	app.Namespace("deploy", func() {
		app.Desc("Check that the deploy host can reach the repository")
		app.Task("check", nil, func() error {
			return app.On(app.ReleaseRoles("all"), func(s *Session) error {
				return app.scm.Check(s, c)
			})
		})

		app.Desc("Start a deployment")
		app.Task("starting", nil, nil)
		app.Task("started", nil, nil)

		app.Desc("Update server(s) with a new release")
		app.Task("updating", nil, func() error {
			c.Set("release_timestamp", app.releaseTimestamp())
			return app.On(app.ReleaseRoles("all"), func(s *Session) error {
				return app.scm.Release(s, c)
			})
		})
		app.Task("updated", nil, nil)

		app.Desc("Publish the release (symlink current)")
		app.Task("publishing", nil, func() error {
			return app.On(app.ReleaseRoles("all"), func(s *Session) error {
				return s.Execute("ln", "-sfn", releasePath(c), currentPath(c))
			})
		})
		app.Task("published", nil, nil)

		app.Desc("Finish the deployment, clean up old releases")
		app.Task("finishing", nil, func() error {
			return app.On(app.ReleaseRoles("all"), func(s *Session) error {
				return s.Execute("cleanup-releases", deployTo(c))
			})
		})
		app.Task("finished", nil, nil)

		app.Desc("Log details of the deploy")
		app.Task("log_revision", nil, func() error {
			return app.On(app.ReleaseRoles("all"), func(s *Session) error {
				rev, err := app.scm.Revision(s, c)
				if err != nil {
					return err
				}
				return s.Execute("log-revision", rev, currentPath(c))
			})
		})
	})

	// deploy => the ordered flow; log_revision runs after finishing; check runs
	// before starting.
	app.Task("deploy", []string{
		"deploy:starting", "deploy:started",
		"deploy:updating", "deploy:updated",
		"deploy:publishing", "deploy:published",
		"deploy:finishing", "deploy:finished",
	}, nil)
	_ = app.After("deploy:finishing", "deploy:log_revision")
	_ = app.Before("deploy:starting", "deploy:check")
}

// --- small typed-value helpers ----------------------------------------------

// str renders a config value as a string ("" for nil / non-string).
func str(v any) string {
	s, _ := v.(string)
	return s
}

// strOr renders v as a string, falling back to def when v is nil / not a string
// / empty.
func strOr(v any, def string) string {
	if s, ok := v.(string); ok && s != "" {
		return s
	}
	return def
}
