# Codex Capacity Auto-Retry Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Automatically and immediately retry only the exact Codex selected-model capacity error, with six additional retries by default and deployment-level configuration.

**Architecture:** Keep classification and retry eligibility separate. `pkg/taskfailure` recognizes and normalizes the exact raw error for reporting, while `TaskService` owns a dedicated raw-error retry policy that reuses the atomic `FailTask -> CreateRetryTask` flow and persists `retry_count + 1` as the child ceiling. The API composition boundary parses `MULTICA_CODEX_CAPACITY_RETRY_COUNT` and injects it into the service; deployment manifests only expose the value.

**Tech Stack:** Go 1.26, pgx/sqlc task persistence, Go testing, Docker Compose, Helm, Fumadocs MDX.

---

## File Map

- `server/pkg/taskfailure/classify.go`: exact Codex capacity witness and legacy-daemon normalization.
- `server/pkg/taskfailure/classify_test.go`: strict equality and normalization regression tests.
- `server/internal/service/task.go`: capacity retry budget, eligibility, immediate scheduling, and sweeper parity.
- `server/internal/service/task_complete_race_test.go`: pure retry-policy matrix and generic-policy non-regression.
- `server/internal/service/retry_deferred_test.go`: database-backed `FailTask` child creation assertions.
- `server/internal/handler/handler.go`: handler config field and `TaskService` injection.
- `server/cmd/server/router.go`: deployment environment parsing and handler configuration.
- `server/cmd/server/codex_capacity_retry_test.go`: environment parser tests.
- `.env.example`: operator-facing default.
- `docker-compose.selfhost.yml`: backend environment forwarding.
- `deploy/helm/multica/values.yaml`: Helm backend configuration default.
- `deploy/helm/multica/templates/configmap.yaml`: Helm environment rendering.
- `apps/docs/content/docs/environment-variables{,.zh,.ja,.ko}.mdx`: localized API-server configuration reference.

### Task 1: Make The Capacity Witness Strict

**Files:**
- Modify: `server/pkg/taskfailure/classify_test.go`
- Modify: `server/pkg/taskfailure/classify.go`

- [ ] **Step 1: Add failing strict-match tests**

Extend the classifier and normalization tables with exact and near-match cases:

```go
const codexCapacityError = "Selected model is at capacity. Please try a different model."

{"codex selected model at capacity", codexCapacityError, ReasonAgentProviderCapacityOrRateLimit},
{"codex capacity uppercase stays model unavailable", strings.ToUpper(codexCapacityError), ReasonAgentModelNotFoundOrUnavailable},
{"codex capacity padded stays model unavailable", " " + codexCapacityError, ReasonAgentModelNotFoundOrUnavailable},
{"codex capacity suffix stays model unavailable", codexCapacityError + " Contact support.", ReasonAgentModelNotFoundOrUnavailable},
```

For `NormalizeDaemonReason`, assert that only the exact raw error upgrades legacy
`model_not_found_or_unavailable`, `unknown`, and coarse `agent_error` labels.

- [ ] **Step 2: Run the focused tests and confirm RED**

```powershell
Set-Location server
go test ./pkg/taskfailure -run 'Test(ClassifyRules|NormalizeDaemonReason)$' -count=1
```

Expected: FAIL because the current predicate trims and folds case.

- [ ] **Step 3: Implement exact recognition**

```go
const codexSelectedModelCapacityError = "Selected model is at capacity. Please try a different model."

func IsCodexSelectedModelCapacityError(rawError string) bool {
	return rawError == codexSelectedModelCapacityError
}
```

Call this predicate from `Classify(rawError)` before the broader selected-model
rule and from `NormalizeDaemonReason`. Do not compare lowercased or trimmed input
for this witness.

- [ ] **Step 4: Run the focused tests and confirm GREEN**

Run the command from Step 2. Expected: PASS.

- [ ] **Step 5: Commit the classifier increment**

```powershell
git commit --only -m "fix(taskfailure): recognize exact Codex capacity error" -- server/pkg/taskfailure/classify.go server/pkg/taskfailure/classify_test.go
```

### Task 2: Add The Dedicated Capacity Retry Policy

**Files:**
- Modify: `server/internal/service/task_complete_race_test.go`
- Modify: `server/internal/service/retry_deferred_test.go`
- Modify: `server/internal/service/task.go`

- [ ] **Step 1: Rewrite pure policy tests to the approved budget semantics**

Pass an explicit capacity count into the retry helpers. Cover attempts 1 through
7, generic `max_attempts=1`, count zero, strict near-matches, generic rate
limits, autopilot exclusion, and missing issue/chat links. Core assertions:

```go
const capacityErr = "Selected model is at capacity. Please try a different model."
const capacityRetryCount int32 = 6

if !retryEligible(reason, capacityErr, mkTask(1, 1), capacityRetryCount) {
	t.Fatal("capacity policy must override generic max_attempts")
}
if retryEligible(reason, capacityErr, mkTask(7, 1), capacityRetryCount) {
	t.Fatal("attempt 7 must exhaust six additional retries")
}
if retryEligible(reason, capacityErr, mkTask(1, 2), 0) {
	t.Fatal("zero capacity retries must disable the special policy")
}
if got := retryAttemptCeiling(reason, capacityErr, 1, capacityRetryCount); got != 7 {
	t.Fatalf("ceiling = %d, want 7", got)
}
if got := retryDelayForAttempt(reason, capacityErr, 6); got != 0 {
	t.Fatalf("capacity delay = %s, want immediate", got)
}
```

Update every generic retry test to pass empty raw error and zero capacity count
so provider-network and timeout behavior remain independently covered.

- [ ] **Step 2: Add a database-backed failing `FailTask` test**

Add `TestFailTaskCodexCapacityBudget` beside the provider-network test. Create a
running parent with `attempt=1/max_attempts=1`, configure the service with count
six, call `FailTask` with the exact raw error and a legacy model-unavailable
reason, and assert one child with:

```go
wantAttempt := int32(2)
wantMaxAttempts := int32(7)
wantStatus := "queued"
wantFireAtValid := false
wantForceFreshSession := false
```

Table cases also assert no child for count zero, attempt seven, and a generic
429 error.

- [ ] **Step 3: Run focused service tests and confirm RED**

```powershell
Set-Location server
go test ./internal/service -run 'Test(ProviderNetworkRetrySchedule|TaskFailureClassifiers|CodexCapacityFailureRetries|FailTaskCodexCapacityBudget)$' -count=1
```

Expected: compile failure from the helper signatures or assertion failures
because capacity still uses the generic budget.

- [ ] **Step 4: Implement the minimal service policy**

```go
const DefaultCodexCapacityRetryCount int32 = 6

// Add to TaskService.
CodexCapacityRetryCount int32

func retryCandidate(reason, rawError string, capacityRetryCount int32) bool {
	if taskfailure.IsCodexSelectedModelCapacityError(rawError) {
		return capacityRetryCount > 0
	}
	return retryableReasons[reason]
}

func retryAttemptCeiling(reason, rawError string, taskMaxAttempts, capacityRetryCount int32) int32 {
	if taskfailure.IsCodexSelectedModelCapacityError(rawError) && capacityRetryCount > 0 {
		return capacityRetryCount + 1
	}
	if taskMaxAttempts <= 1 {
		return taskMaxAttempts
	}
	if reason == string(taskfailure.ReasonAgentProviderNetwork) && taskMaxAttempts < providerNetworkMaxAttempts {
		return providerNetworkMaxAttempts
	}
	return taskMaxAttempts
}

func retryDelayForAttempt(reason, rawError string, failedAttempt int32) time.Duration {
	if taskfailure.IsCodexSelectedModelCapacityError(rawError) {
		return 0
	}
	if reason == string(taskfailure.ReasonAgentProviderNetwork) &&
		failedAttempt >= providerNetworkMaxAttempts-1 {
		return providerNetworkFinalRetryWait
	}
	return 0
}

func retryEligible(reason, rawError string, task db.AgentTaskQueue, capacityRetryCount int32) bool {
	return retryCandidate(reason, rawError, capacityRetryCount) &&
		task.Attempt < retryAttemptCeiling(reason, rawError, task.MaxAttempts, capacityRetryCount) &&
		!task.AutopilotRunID.Valid &&
		(task.IssueID.Valid || task.ChatSessionID.Valid)
}
```

In `FailTask`, use `errMsg` and the injected setting for candidate, eligibility,
ceiling, and delay decisions. In
`MaybeRetryFailedTask`, use `parent.Error.String` and the same setting. Leave
capacity out of `retryableReasons`.

- [ ] **Step 5: Format and run focused tests GREEN**

```powershell
gofmt -w server/internal/service/task.go server/internal/service/task_complete_race_test.go server/internal/service/retry_deferred_test.go
Set-Location server
go test ./internal/service -run 'Test(ProviderNetworkRetrySchedule|TaskFailureClassifiers|CodexCapacityFailureRetries|FailTaskProviderNetworkBudget|FailTaskCodexCapacityBudget)$' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit the retry-policy increment**

```powershell
git commit --only -m "feat(task): retry exact Codex capacity failures" -- server/internal/service/task.go server/internal/service/task_complete_race_test.go server/internal/service/retry_deferred_test.go
```

### Task 3: Parse And Inject The Deployment Setting

**Files:**
- Create: `server/cmd/server/codex_capacity_retry_test.go`
- Modify: `server/cmd/server/router.go`
- Modify: `server/internal/handler/handler.go`

- [ ] **Step 1: Write failing environment parser tests**

```go
func TestCodexCapacityRetryCountFromEnv(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want int32
	}{
		{"unset uses default", "", 6},
		{"zero disables", "0", 0},
		{"positive count", "3", 3},
		{"surrounding whitespace", " 3 ", 3},
		{"negative uses default", "-1", 6},
		{"malformed uses default", "six", 6},
		{"maximum accepted", "2147483646", 2147483646},
		{"overflowing total uses default", "2147483647", 6},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("MULTICA_CODEX_CAPACITY_RETRY_COUNT", tc.raw)
			if got := codexCapacityRetryCountFromEnv(); got != tc.want {
				t.Fatalf("codexCapacityRetryCountFromEnv() = %d, want %d", got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run the parser test and confirm RED**

```powershell
Set-Location server
go test ./cmd/server -run TestCodexCapacityRetryCountFromEnv -count=1
```

Expected: compile failure because the parser does not exist.

- [ ] **Step 3: Implement parsing and injection**

Add a domain-specific parser in `router.go` that trims the value, uses
`strconv.ParseInt(raw, 10, 32)`, accepts `0..2147483646`, and logs a warning
before returning `service.DefaultCodexCapacityRetryCount` for invalid input.

Add to `handler.Config`:

```go
CodexCapacityRetryCount int32
```

Populate it in `NewRouterWithOptions` and inject it in `handler.New`:

```go
CodexCapacityRetryCount: codexCapacityRetryCountFromEnv(),
taskSvc.CodexCapacityRetryCount = cfg.CodexCapacityRetryCount
```

- [ ] **Step 4: Format and run parser plus service tests GREEN**

```powershell
gofmt -w server/cmd/server/router.go server/cmd/server/codex_capacity_retry_test.go server/internal/handler/handler.go
Set-Location server
go test ./cmd/server -run TestCodexCapacityRetryCountFromEnv -count=1
go test ./internal/service -run 'Test(CodexCapacityFailureRetries|FailTaskCodexCapacityBudget)$' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit the configuration increment**

```powershell
git commit --only -m "feat(server): configure Codex capacity retries" -- server/cmd/server/router.go server/cmd/server/codex_capacity_retry_test.go server/internal/handler/handler.go
```

### Task 4: Expose The Deployment Configuration

**Files:**
- Modify: `.env.example`
- Modify: `docker-compose.selfhost.yml`
- Modify: `deploy/helm/multica/values.yaml`
- Modify: `deploy/helm/multica/templates/configmap.yaml`
- Modify: `apps/docs/content/docs/environment-variables.mdx`
- Modify: `apps/docs/content/docs/environment-variables.zh.mdx`
- Modify: `apps/docs/content/docs/environment-variables.ja.mdx`
- Modify: `apps/docs/content/docs/environment-variables.ko.mdx`

- [ ] **Step 1: Run manifest assertions before editing**

```powershell
docker compose -f docker-compose.selfhost.yml config | Select-String 'MULTICA_CODEX_CAPACITY_RETRY_COUNT: "6"'
helm template multica deploy/helm/multica | Select-String 'MULTICA_CODEX_CAPACITY_RETRY_COUNT: "6"'
```

Expected: no match.

- [ ] **Step 2: Wire defaults through deployment surfaces**

```dotenv
MULTICA_CODEX_CAPACITY_RETRY_COUNT=6
```

```yaml
MULTICA_CODEX_CAPACITY_RETRY_COUNT: ${MULTICA_CODEX_CAPACITY_RETRY_COUNT:-6}
```

```yaml
backend:
  config:
    codexCapacityRetryCount: 6
```

```yaml
MULTICA_CODEX_CAPACITY_RETRY_COUNT: {{ .Values.backend.config.codexCapacityRetryCount | quote }}
```

- [ ] **Step 3: Document server-side semantics in every locale**

Add the variable to an API-server task retry section, not the daemon table. Each
description states that the default is six additional immediate retries, zero
disables the exact-error policy, and other capacity/rate-limit errors are not
retried.

- [ ] **Step 4: Render and validate manifests GREEN**

```powershell
docker compose -f docker-compose.selfhost.yml config | Select-String 'MULTICA_CODEX_CAPACITY_RETRY_COUNT: "6"'
helm template multica deploy/helm/multica | Select-String 'MULTICA_CODEX_CAPACITY_RETRY_COUNT: "6"'
git diff --check
```

Expected: both render checks match and `git diff --check` reports no errors.

- [ ] **Step 5: Commit deployment and documentation**

```powershell
git commit --only -m "docs: expose Codex capacity retry setting" -- .env.example docker-compose.selfhost.yml deploy/helm/multica/values.yaml deploy/helm/multica/templates/configmap.yaml apps/docs/content/docs/environment-variables.mdx apps/docs/content/docs/environment-variables.zh.mdx apps/docs/content/docs/environment-variables.ja.mdx apps/docs/content/docs/environment-variables.ko.mdx
```

### Task 5: Final Verification And Scope Review

**Files:**
- Verify all files listed above.

- [ ] **Step 1: Run focused backend suites**

```powershell
Set-Location server
go test ./pkg/taskfailure -count=1
go test ./cmd/server -count=1
go test ./internal/service -count=1
```

Expected: PASS for every package.

- [ ] **Step 2: Run broader Go verification**

```powershell
Set-Location server
go test ./... -count=1
```

Expected: PASS. If the repository-wide suite exceeds the execution window,
record the timeout separately from focused passing evidence.

- [ ] **Step 3: Review diff and generated configuration**

```powershell
git diff --check
git status --short
docker compose -f docker-compose.selfhost.yml config
helm template multica deploy/helm/multica
```

Expected: no whitespace errors, the environment variable appears in both
deployment outputs, and unrelated pre-existing worktree changes remain intact.

- [ ] **Step 4: Run GitNexus change detection**

Run `detect_changes({scope: "compare", base_ref: "main"})`, review every changed
symbol and affected process, and investigate any HIGH or CRITICAL result before
completion.

- [ ] **Step 5: Report completed behavior**

Report the strict matching rule, default/configured budget, immediate retry
behavior, COM-44 evidence, deployment surfaces, exact test results, and any
residual verification timeout.
