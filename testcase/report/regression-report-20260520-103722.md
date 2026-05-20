## Scenario Results
- TC-052 onboarding v2 runtime skip | failed | 117s | first failing step: click "暂时跳过" on runtime connection step

## Summary
- Testcase source: OPE-1005 / PR !178 branch `feat/ope-1005-upgrade-v0.3.3`
- Local repo root: `/Users/jiangjiangdear/Desktop/harness/ope-1005-upgrade-v0.3.3`
- Tested commit: `cf7919b1 fix(onboarding): resolve stale-closure in handleRuntimeNext skip path (OPE-1005)`
- Testcase dir: `/Users/jiangjiangdear/Desktop/harness/ope-1005-upgrade-v0.3.3/testcase`
- Target URL: `http://localhost:3001`
- Auth file: `/Users/jiangjiangdear/Desktop/harness/ope-1005-upgrade-v0.3.3/testcase/auth/auth.json`
- Browser session: `abtp-ope-1005-retest-20260520-103100` (closed)
- Docker project: `ope-1005-retest`, ports frontend/backend/postgres = 3001/8081/5433
- Total duration: ~12 min including environment recovery after backend alias correction
- Coverage: 1/1 planned scenario executed
- Pass rate: 0/1
- Passed: none
- Failed: TC-052
- Blocked: none after local backend alias was corrected

## QA Gate Coverage Analysis
- covered_existing: `testcase/case/tc-052-onboarding-v2-questionnaire.md` covers onboarding v2 per-question source/role/use-case flow, workspace creation, and reaching the normal workspace experience after runtime handling. This is the same user-visible path targeted by the dev fix.
- covered_new: no testcase files were added or updated in this retest. Dev added `packages/views/onboarding/onboarding-flow-skip.test.tsx`, which covers the intended component path but is not a browser testcase doc.
- not_browser_applicable: `pnpm typecheck`, web build, backend build, and the component-level unit/integration tests are non-browser checks.
- blocked: none for testcase coverage assessment.

## Static / Build Checks
- PASS: `pnpm --filter @multica/views exec vitest run onboarding-flow-skip.test.tsx` — 1 file / 1 test passed.
- PASS: `pnpm --filter @multica/views exec vitest run onboarding` — 8 files / 34 tests passed.
- PASS: `pnpm --filter @multica/web build` — Next build completed successfully.
- PASS: `cd server && go build ./...` — completed with exit code 0.
- FAIL: `pnpm typecheck` — `@multica/views` failed with `onboarding/onboarding-flow-skip.test.tsx(90,14): error TS6133: 'data' is declared but its value is never read.`

## Browser Execution Details
1. Opened `http://localhost:3001` and logged in with `tester@multica.com` + fixed code `888888`.
2. Entered onboarding welcome page and clicked `在 web 端继续`.
3. Skipped source, role, and use-case question steps.
4. Created workspace `Retest Workspace` (`slug=retest-workspace`, backend workspace id `31acfa6d-26dc-4b66-88f9-93f0df01a54c`).
5. Reached runtime connection step with visible enabled button `暂时跳过`.
6. Clicked `暂时跳过` by element ref, waited for network idle, and observed no transition.
7. Clicked `暂时跳过` again by exact visible text and waited 10 seconds.
8. Network requests still showed only repeated `GET /api/runtimes?workspace_id=31acfa6d-26dc-4b66-88f9-93f0df01a54c&owner=me` 200 requests. No `POST /api/me/onboarding/no-runtime-bootstrap` request appeared.
9. URL remained `http://localhost:3001/onboarding`; UI stayed on the runtime connection step.

## Evidence
- Screenshot: `/Users/jiangjiangdear/Desktop/harness/ope-1005-upgrade-v0.3.3/testcase/report/images/tc-052-runtime-skip-stuck-retest-20260520-103600.png`
- Network evidence: no `no-runtime-bootstrap` request after two clicks; runtime polling continued.
- Backend log evidence: workspace creation succeeded; subsequent logs only show runtime polling, no `POST /api/me/onboarding/no-runtime-bootstrap`.

## Failed Case Handback
- Scenario: `TC-052 onboarding v2 runtime skip`
- Expected result: clicking `暂时跳过` calls `POST /api/me/onboarding/no-runtime-bootstrap`, creates or reuses the self-serve onboarding issue, marks onboarding complete, and navigates to the workspace issue/detail experience.
- Actual result: button is visible and enabled, but clicking it does not trigger `POST /api/me/onboarding/no-runtime-bootstrap`; page remains on `/onboarding` runtime step.
- First failing step: runtime connection step, click `暂时跳过`.
- Evidence: screenshot path above plus network/backend logs.
- Development handback: return to development. The new component test passes, so the production browser path likely differs from the mocked test path. Re-check the actual rendered `StepPlatformFork` callback binding and whether `onNext(null)` reaches `handleRuntimeNext` in the built app. Also fix the new test's unused `data` parameter so `pnpm typecheck` passes.

## Cleanup
- Browser session closed.
- Docker cleanup was run after report generation; see final issue comment for cleanup result.
