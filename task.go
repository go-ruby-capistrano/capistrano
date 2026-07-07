// Copyright (c) the go-ruby-capistrano/capistrano authors
//
// SPDX-License-Identifier: BSD-3-Clause

package capistrano

import (
	"sort"
	"strings"
)

// TaskBody is a task's action body — the seam for a Capistrano `task … do …
// end` block. A host (rbgo) wires each body to the Ruby block; tests pass Go
// closures. Returning an error aborts the invocation (an exception escaping the
// block), and the error is remembered so a later invoke re-raises it.
type TaskBody func() error

// Task is one unit of work in the graph — the Capistrano/Rake Task. It carries a
// fully-qualified (namespace-prefixed) name, its ordinary prerequisites, its
// after-hooks (invoked once its own actions succeed), the action bodies, a
// description, and the invoke-once guard with its remembered error.
type Task struct {
	name          string
	prerequisites []string
	after         []string
	actions       []TaskBody
	desc          string
	invoked       bool
	err           error
}

// Name returns the task's fully-qualified name.
func (t *Task) Name() string { return t.name }

// Prerequisites returns the ordinary prerequisite names, in invoke order.
func (t *Task) Prerequisites() []string { return append([]string(nil), t.prerequisites...) }

// Description returns the task's desc text (Rake::Task#comment).
func (t *Task) Description() string { return t.desc }

// Reenable clears the invoke-once guard so the task runs again (Task#reenable,
// the mechanism behind invoke!).
func (t *Task) Reenable() {
	t.invoked = false
	t.err = nil
}

// TaskManager is the task registry and namespace resolver — the Rake half of
// Capistrano's DSL. It owns the task table (keyed by fully-qualified name), the
// live namespace stack, and the pending description, and drives invoke with its
// depth-first, prerequisite-first, invoke-once walk plus circular detection.
type TaskManager struct {
	tasks    map[string]*Task
	scope    []string
	lastDesc string
}

// NewTaskManager returns an empty registry at the top-level scope.
func NewTaskManager() *TaskManager {
	return &TaskManager{tasks: map[string]*Task{}}
}

// Desc records the description applied to the next defined task (the `desc`
// DSL); it is consumed by the following Define, then cleared.
func (m *TaskManager) Desc(text string) { m.lastDesc = text }

// Namespace evaluates body with name pushed on the scope stack (the `namespace`
// DSL), so tasks defined inside are prefixed "name:". Namespaces nest.
func (m *TaskManager) Namespace(name string, body func()) {
	m.scope = append(m.scope, name)
	defer func() { m.scope = m.scope[:len(m.scope)-1] }()
	if body != nil {
		body()
	}
}

// scoped prefixes name with the current namespace path (Rake scope resolution
// at definition time). An already-qualified name (":"-containing) is left as-is
// only when it is looked up; at definition the current scope always applies.
func (m *TaskManager) scoped(name string) string {
	if len(m.scope) == 0 {
		return name
	}
	return strings.Join(m.scope, ":") + ":" + name
}

// Define registers (or, when it already exists, enhances) a task named within
// the current namespace, with the given prerequisites and body — the `task` DSL.
// Re-defining a task appends its prerequisites (union) and its body, matching
// Rake's define_task enhancement.
func (m *TaskManager) Define(name string, deps []string, body TaskBody) *Task {
	fq := m.scoped(name)
	t, ok := m.tasks[fq]
	if !ok {
		t = &Task{name: fq}
		m.tasks[fq] = t
	}
	for _, d := range deps {
		if !containsStr(t.prerequisites, m.resolveName(d)) {
			t.prerequisites = append(t.prerequisites, m.resolveName(d))
		}
	}
	if body != nil {
		t.actions = append(t.actions, body)
	}
	if m.lastDesc != "" {
		t.desc = m.lastDesc
		m.lastDesc = ""
	}
	return t
}

// resolveName qualifies a referenced task name against the current scope: an
// absolute name (already containing ":") is used verbatim; a bare name is
// resolved to the current namespace if such a task exists there, else left bare
// (a reference to a top-level task).
func (m *TaskManager) resolveName(name string) string {
	if strings.Contains(name, ":") || len(m.scope) == 0 {
		return name
	}
	scoped := strings.Join(m.scope, ":") + ":" + name
	if _, ok := m.tasks[scoped]; ok {
		return scoped
	}
	return name
}

// Lookup resolves a task by fully-qualified name (Rake::Task[]). It returns the
// task and whether it was found.
func (m *TaskManager) Lookup(name string) (*Task, bool) {
	t, ok := m.tasks[name]
	return t, ok
}

// Tasks returns every registered task, sorted by name.
func (m *TaskManager) Tasks() []*Task {
	out := make([]*Task, 0, len(m.tasks))
	for _, t := range m.tasks {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

// Before makes prerequisite run before task — the `before` DSL. Capistrano
// prepends it to task's prerequisites, so the most-recently-declared before-hook
// runs first. Both names are resolved against the current scope.
func (m *TaskManager) Before(task, prerequisite string) error {
	t, ok := m.tasks[m.resolveName(task)]
	if !ok {
		return &TaskNotFoundError{Name: task}
	}
	t.prerequisites = append([]string{m.resolveName(prerequisite)}, t.prerequisites...)
	return nil
}

// After makes post run after task's own actions — the `after` DSL. Capistrano
// enhances task to invoke post once its body has run.
func (m *TaskManager) After(task, post string) error {
	t, ok := m.tasks[m.resolveName(task)]
	if !ok {
		return &TaskNotFoundError{Name: task}
	}
	t.after = append(t.after, m.resolveName(post))
	return nil
}

// Invoke runs the task, prerequisites first, honouring the invoke-once guard —
// the `invoke` DSL. A task already invoked is skipped (its remembered error, if
// any, is re-raised).
func (m *TaskManager) Invoke(name string) error {
	return m.invoke(m.resolveName(name), nil)
}

// InvokeBang re-enables the task (only) and invokes it, forcing it to run again
// even if already invoked — the `invoke!` DSL (Rake's invoke!).
func (m *TaskManager) InvokeBang(name string) error {
	fq := m.resolveName(name)
	if t, ok := m.tasks[fq]; ok {
		t.Reenable()
	}
	return m.invoke(fq, nil)
}

// invoke is the recursive core: detect a cycle against the call chain, honour
// the once-guard (re-raising a remembered failure), then run prerequisites
// depth-first, the task's own actions, and finally the after-hooks — each under
// the growing chain, each remembering a failure.
func (m *TaskManager) invoke(name string, chain []string) error {
	t, ok := m.tasks[name]
	if !ok {
		return &TaskNotFoundError{Name: name}
	}
	if containsStr(chain, name) {
		return &Error{Message: "Circular dependency detected: " +
			strings.Join(append(chain, name), " => ")}
	}
	if t.invoked {
		return t.err
	}
	t.invoked = true
	next := append(append([]string(nil), chain...), name)
	for _, p := range t.prerequisites {
		if err := m.invoke(p, next); err != nil {
			t.err = err
			return err
		}
	}
	for _, act := range t.actions {
		if err := act(); err != nil {
			t.err = err
			return err
		}
	}
	for _, post := range t.after {
		if err := m.invoke(post, next); err != nil {
			t.err = err
			return err
		}
	}
	return nil
}

// containsStr reports whether list contains s (the |=-style membership used for
// prerequisite/chain de-duplication).
func containsStr(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
