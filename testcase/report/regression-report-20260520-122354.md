## Scenario Results
- TC-052 | failed | 2 fresh browser sessions | first failing step: click `暂时跳过` on runtime connection step
- Onboarding smoke | passed until runtime step | login, welcome, source/role/use-case skip, workspace creation, runtime page render all passed

## Summary
- Testcase source: `/Users/jiangjiangdear/Desktop/harness/ope-1005-upgrade-v0.3.3`
- Local repo root: `/Users/jiangjiangdear/Desktop/harness/ope-1005-upgrade-v0.3.3`
- Branch / commit: `feat/ope-1005-upgrade-v0.3.3` / `5643703a`
- Testcase dir: `testcase/`
- Target URL: `http://localhost:3001`
- Auth file: `testcase/auth/auth.json`
- Sessions: `abtp-ope-1005-retest2-20260520-120420`, `abtp-ope-1005-retest3-20260520-121650`
- Session reuse: none; both were fresh sessions
- Report file: `testcase/report/regression-report-20260520-122354.md`
- Total duration: about 28 minutes, including local Docker provision and two browser passes
- Coverage: `2/2` planned scenarios executed
- Pass rate: `1/2`
- Passed: onboarding smoke through runtime step render
- Failed: `tc-052-onboarding-v2-questionnaire` runtime skip user-click path
- Blocked: none

## Coverage Check
- New or updated testcase files: none.
- Reason no testcase update was needed: this retest targets the existing failure in `testcase/case/tc-052-onboarding-v2-questionnaire.md`; that testcase already covers onboarding v2, workspace creation, runtime connection, and reaching the normal workspace experience.
- Existing testcase docs used:
  - `testcase/case/tc-052-onboarding-v2-questionnaire.md`
  - `testcase/browser-regression-guide.md`
- Multica issue check: workspace issue search found OPE-1005 as the relevant active issue and no separate completed duplicate for onboarding runtime skip.
- Official GitHub issue check: `gh issue list` searches for `onboarding runtime skip`, `onboarding "Skip for now"`, and `"no-runtime-bootstrap"` found no matching upstream fixed issue.
- Official source check: `~/Desktop/harness/multica-official-upstream` was fetched and checked out to `github/main`; upstream still reads `workspace` directly in `handleRuntimeNext`, while this PR adds the `workspaceRef` / `onCompleteRef` patch.

## Build And Static Verification
- PASS `pnpm --filter @multica/views exec vitest run onboarding`: 8 files / 34 tests passed.
- PASS `pnpm typecheck`: 7/7 packages passed.
- PASS `pnpm --filter @multica/web build`: Next.js build succeeded.
- PASS `cd server && go build ./...`: Go backend build succeeded.

## Browser Regression

### Environment
- Docker project: `ope-1005-retest2`
- Ports: frontend `3001`, backend `8081`, postgres `5433`
- Backend: `APP_ENV=development`, `MULTICA_DEV_VERIFICATION_CODE=888888`
- Frontend readiness: `http://localhost:3001` returned 200
- Backend proxy readiness: `http://localhost:3001/api/config` returned 200

### Onboarding Smoke
- Login using `tester@multica.com` / `888888`: passed.
- Welcome step displayed and `在 web 端继续` advanced: passed.
- Source, role, and use-case appeared as separate question steps: passed.
- `跳过` on each question advanced to the next step: passed.
- Workspace creation returned 201 and rendered the runtime connection step: passed.

### TC-052 Runtime Skip Retest
- First fresh run:
  - User: `tester@multica.com`
  - Workspace: `Retest Two Workspace`
  - Click method: `agent-browser click @e6` and `agent-browser find text "暂时跳过" click --exact`
  - Actual result: page stayed at `/onboarding`; network showed only repeated `GET /api/runtimes?...` 200; no `POST /api/me/onboarding/no-runtime-bootstrap`.
  - Diagnostic note: a later page-context `button.click()` on the same button did trigger `POST /api/me/onboarding/no-runtime-bootstrap` 200 and navigated to `/retest-two-workspace/issues/RET-1`. This proves backend and callback target can work, but it is not a valid user gesture substitute for the regression result.
- Second fresh run:
  - User: `tester2@multica.com`
  - Workspace: `Retest Three Workspace`
  - Click method: `agent-browser click @e6`
  - Actual result: page stayed at `/onboarding`; backend log and network capture show only repeated `GET /api/runtimes?...` 200 after the click; no `POST /api/me/onboarding/no-runtime-bootstrap`.

## Evidence
- Failure screenshot: `testcase/report/images/tc-052-runtime-skip-stuck-retest3-20260520-122150.png`
- Pre-click screenshot: `testcase/report/images/tc-052-runtime-step-before-real-click-retest3-20260520-121650.png`
- First-run failure screenshot: `testcase/report/images/tc-052-runtime-skip-stuck-retest2-20260520-121500.png`
- Network capture: `testcase/report/tc-052-retest3-network-20260520-122150.txt`
- Backend log capture: `testcase/report/tc-052-retest3-backend-20260520-122150.log`

## Development Handback
- Scenario: `tc-052-onboarding-v2-questionnaire`
- Expected result: after clicking `暂时跳过`, frontend sends `POST /api/me/onboarding/no-runtime-bootstrap`, creates or reuses the no-runtime onboarding issue, marks onboarding complete, and navigates into the workspace issue page.
- Actual result: in two fresh sessions, clicking `暂时跳过` as a user gesture leaves the app on `/onboarding` at the runtime step. Network and backend logs show no `POST /api/me/onboarding/no-runtime-bootstrap`, only the runtime polling loop.
- First failing step: click `暂时跳过` on the runtime connection step.
- Evidence: screenshots and network/backend captures listed above.
- Initial localization: backend endpoint works and the final `handleRuntimeNext(null)` path can be reached by direct page-context `button.click()`, but the real browser click path from the visible button is still not reliably delivered to the handler. Recheck the runtime step's user gesture path, pointer/overlay behavior, button composition, and whether the visible button is receiving a click that React handles.
