Purpose: Verify that the auth middleware correctly handles task tokens (mat_ prefix), strips client-supplied X-Actor-Source headers (CSRF defense), and maintains cookie fallback for invalid bearer tokens.

Preconditions: The Multica backend is reachable. A valid task token (mat_...) exists from a running or completed agent task. A valid PAT (mul_...) exists. The user has a valid session cookie.

User flow:
1. (API-level) Send a request with Authorization: Bearer mat_<valid_task_token>. Verify the response succeeds and the request is attributed to the correct user/agent/task (check via response headers or audit).
2. (API-level) Send a request with a client-supplied X-Actor-Source header alongside a valid PAT. Verify the response does NOT reflect the client-supplied X-Actor-Source — it should be stripped.
3. (API-level) Send a request with an invalid bearer token but a valid session cookie. Verify the request falls through to cookie auth and succeeds (fallback behavior).
4. (Browser) Sign in normally via the web app. Perform an action (e.g. create an issue). Verify it succeeds — this exercises the cookie auth path and confirms CSRF protection does not block legitimate browser requests.
5. (API-level) Send a request with Authorization: Bearer mat_<invalid_token>. Verify it returns 401/falls through, not a 500.

Expected results:
- Task tokens (mat_) are validated against the DB hash and set X-User-ID, X-Agent-ID, X-Task-ID, X-Workspace-ID, X-Actor-Source=task_token on the request context.
- Client-supplied X-Actor-Source is always stripped before auth processing.
- Invalid bearer tokens fall through to the next candidate (cookie), preserving the Fork's fallback behavior.
- PAT tokens are cached after first validation (patCache.Set is called).
- Normal browser session auth continues to work without interference from the new task token logic.

Notes for automation: Steps 1-3, 5 require direct HTTP requests (curl or API client). Step 4 can be verified via normal browser interaction. The X-Actor-Source stripping is not directly observable from the client but can be inferred by ensuring no privilege escalation occurs when the header is injected.
