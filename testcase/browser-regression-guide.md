## Browser Regression Guide

This suite is prepared for later execution by `agent-browser` through the Multica regression test runner skill. This stage only defines repository-local browser cases and does not execute them.

Testcase files live under `testcase/case/` and should be read in lexicographic filename order. Each `tc-*.md` file contains exactly one browser regression scenario.

This repository currently does not provide `testcase/ui-selectors.json`, so cases should be executed directly from scenario prose using visible UI semantics. Resolve controls by page titles, button text, form labels, placeholder text, dialog text, and final page state. Selector authoring is optional follow-up work, not a prerequisite for execution.

Fallback order for later execution is:
1. The visible UI target described in the testcase step or sentence.
2. Accessible role and name, label text, placeholder text, or obvious page semantics.
3. Stable business-state assertions such as the authenticated redirect target and the presence of Issues page content.

Authentication data for this feature is already stored in `testcase/auth/auth.json`. For the fixed verification code login case, use `tester@multica.com` with verification code `888888`, then verify that the browser leaves `/login` and reaches the default authenticated Issues route.

Browser-observable assertions are preferred over implementation details. For this feature, the key proof points are the login card transition from email entry to code entry, the absence of visible login errors, and the final authenticated Issues page after successful verification.
