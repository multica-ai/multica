# OpenCode Task Workdir Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ensure OpenCode-backed tasks always run in the Multica task workdir, including attach/server mode where OpenCode resolves `--dir .` through `PWD`.

**Architecture:** Keep the behavior inside the OpenCode backend because this is provider-specific command construction. The daemon already passes `ExecOptions.Cwd`; OpenCode must translate that into `cmd.Dir`, `PWD=<cwd>`, and an explicit `--dir <cwd>` argument while blocking user custom args from overriding daemon-owned directory control.

**Tech Stack:** Go backend, `server/pkg/agent`, Go unit tests with a test-binary helper process.

---

## Task 1: Capture OpenCode Invocation

**Files:**
- Modify: `server/pkg/agent/opencode_test.go`
- Test: `server/pkg/agent/opencode_test.go`

- [ ] **Step 1: Add a helper-mode `TestMain`**

Add a `TestMain` to `server/pkg/agent/opencode_test.go` that exits early when `MULTICA_TEST_OPENCODE_HELPER=1`. In helper mode it writes `os.Args[1:]`, `os.Getenv("PWD")`, and `os.Getwd()` to `MULTICA_TEST_OPENCODE_CAPTURE`, then prints one valid OpenCode JSON text event.

- [ ] **Step 2: Add the failing regression test**

Add `TestOpencodeExecutePinsTaskWorkdirForAttachMode`. It should run `opencodeBackend.Execute` with:

```go
ExecOptions{
    Cwd: taskDir,
    CustomArgs: []string{"--attach", "http://127.0.0.1:4096", "--dir", ".", "--format", "text"},
}
```

The test expects:

- helper `cwd` equals `taskDir`
- helper `pwd` equals `taskDir`
- captured args include `--dir`, `taskDir`
- captured args do not include the user-supplied `--dir .`
- captured args do not include the user-supplied `--format text`

- [ ] **Step 3: Run RED**

Run:

```bash
cd server && go test ./pkg/agent -run TestOpencodeExecutePinsTaskWorkdirForAttachMode -count=1
```

Expected: fail because current OpenCode backend leaves `PWD` from `Config.Env` and does not add daemon-owned `--dir <taskDir>`.

---

## Task 2: Pin OpenCode Directory Semantics

**Files:**
- Modify: `server/pkg/agent/opencode.go`
- Test: `server/pkg/agent/opencode_test.go`

- [ ] **Step 1: Block custom `--dir`**

Add `--dir` to `opencodeBlockedArgs` with `blockedWithValue`. The daemon owns directory routing for task isolation.

- [ ] **Step 2: Add daemon-owned `--dir <cwd>`**

When `opts.Cwd` is non-empty, append `--dir`, `opts.Cwd` to OpenCode args before filtered custom args.

- [ ] **Step 3: Override child `PWD`**

Build env through a helper that removes existing `PWD` and `OPENCODE_PERMISSION` entries, then appends:

```text
PWD=<opts.Cwd>
OPENCODE_PERMISSION={"*":"allow"}
```

Only set `PWD` when `opts.Cwd` is non-empty.

- [ ] **Step 4: Run GREEN**

Run:

```bash
cd server && go test ./pkg/agent -run TestOpencodeExecutePinsTaskWorkdirForAttachMode -count=1
```

Expected: pass.

---

## Task 3: Regression Suite

**Files:**
- Test: `server/pkg/agent/opencode_test.go`

- [ ] **Step 1: Run OpenCode package tests**

Run:

```bash
cd server && go test ./pkg/agent -run OpenCode -count=1
```

Expected: pass.

- [ ] **Step 2: Run backend agent package tests**

Run:

```bash
cd server && go test ./pkg/agent -count=1
```

Expected: pass.

- [ ] **Step 3: Run backend verification**

Run:

```bash
make test
```

Expected: pass.

---

## Self-Review

- **Spec coverage:** Covers `cmd.Dir`, `PWD`, explicit `--dir <taskWorkdir>`, and blocking user override.
- **Scope:** Does not add project-level workdir policy; that remains M6.
- **Risk:** Adding `--dir` assumes current OpenCode supports it, matching the reported attach/server behavior.
