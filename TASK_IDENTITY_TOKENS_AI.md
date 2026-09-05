# Integrating a Legacy System with Task Identity Tokens (for AI Agents)

This document is designed for AI agents to execute. You are working **inside an
existing internal system** — an ERP, an admin backend, a wiki — and your job is
to make it accept task identity tokens issued by Multica, so that an agent's
request arrives as a **named human** and is authorized by the permission model
that system already has.

Read [TASK_IDENTITY_TOKENS.md](TASK_IDENTITY_TOKENS.md) for what the feature is
and how the server side is configured. This document is only the receiving end.

## The one rule that shapes everything

**You are adding an authentication method, not an authorization model.**

The system already knows who its users are and what each may do. Your change
ends the moment you have turned a token into "this is user `alice`" and handed
that to the code path that already exists for a logged-in Alice. Everything
after that — permissions, roles, row filters, audit — must be the code that was
already there.

If you find yourself writing new permission checks, new roles, or a bypass for
"requests from the bot", stop. That is the failure this feature exists to
prevent.

## Step 1 — Collect these four facts before writing code

Ask the operator, or read them from the deployment's configuration. Do not
guess.

1. **The JWKS URL** — `https://<multica-host>/.well-known/jwks.json`.
   Verify it now: it must return `{"keys":[...]}`. A `404` means task tokens
   are not configured on that server, and you cannot proceed.
2. **The `sub` claim's meaning** — what identifier the deployment templated into
   `sub`, and which column in *this* system it corresponds to. The recommended
   default is the full corporate email (`alice@domain.com`); some deployments use
   the local part (`alice`) against a username column, which is safe only when
   the issuing side restricts signing to the corporate domain. Confirm it; an
   incorrect mapping is a silent privilege error, not a crash.
3. **The expected `iss`, and `aud` if used** — the exact strings this system
   must require. See the warning in Step 4.
4. **Which header carries the token** — `Authorization: Bearer <jwt>` unless the
   operator says otherwise.

Inspect a real token to ground all of this. Decode without verifying, just to
read the shape:

```bash
echo "$TOKEN" | cut -d. -f2 | base64 -d 2>/dev/null | python3 -m json.tool
echo "$TOKEN" | cut -d. -f1 | base64 -d 2>/dev/null | python3 -m json.tool
```

The header tells you the `alg` and `kid` you must handle. The payload tells you
which claims are actually present.

## Step 2 — Fetch and cache the key set

Fetch the JWKS URL and cache it in memory, keyed by `kid`, honouring the
response's `Cache-Control` (the server sends `max-age=300`).

Requirements:

- **Never fetch the JWKS while handling a request** on the hot path without a
  cache. An unreachable Multica must not take your system down.
- **Refetch on an unknown `kid`**, at most once per short interval. This is how
  a rotated key is picked up without a restart.
- **Use a bounded stale cache if a refresh fails**, with a retry cooldown
  even after failures. Set a maximum stale age according to your key-revocation
  requirements; once it expires, reject verification until refresh succeeds.
- **Coalesce concurrent refreshes.** Unknown keys must not turn parallel
  requests into parallel network calls. Requests with cached keys should keep
  working while another request refreshes.
- Do not persist the keys to disk as configuration. The endpoint is the source
  of truth; hand-copied keys are what this endpoint exists to eliminate.

## Step 3 — Verify the token

In order. Every step is required.

1. **Parse the header, read `kid`, look up that key.** If the token has no
   `kid`, use the entry in the set that has no `kid` — the server publishes one
   for templates configured without a key id.
2. **Verify the signature.**
3. **Pin the algorithm.** Accept only the asymmetric algorithms you expect —
   `ES256`, `ES384`, `RS256`, `RS384`. See the warning below; this line is the
   difference between an authentication system and an open door.
4. **Check `exp`.** Allow at most 60s of clock skew. There is no `nbf`.
5. **Check `iss`** against the exact expected string.
6. **Check `aud`** if the deployment templated one.
7. **Check any deployment-specific claim** — e.g. `scope` must equal `erp`.
   This is what stops a token minted for the wiki from opening the ERP.

Only after all of that, read `sub`.

### The algorithm check is not optional

If your verifier accepts whatever `alg` the token header names, two classic
attacks apply, and both are trivial:

- `alg: none` — a token with an empty signature validates.
- `alg: HS256` — the attacker signs with the **public key** as the HMAC secret.
  Your public key is served to anyone who asks, by design. A verifier that
  allows a symmetric algorithm hands out forgery for free.

Pass an explicit algorithm allowlist to your JWT library. Many libraries make
this a required argument precisely because of this; if yours makes it optional,
pass it anyway.

## Step 4 — Map `sub` to a local user

```
sub  →  look up your existing user record  →  the request runs as that user
```

- **A `sub` with no matching local user must be rejected**, with the same status
  your system uses for an unknown user. Do not auto-create accounts. Do not
  fall back to a default or service user. Either the person exists here, or the
  request does not proceed.
- **A disabled, locked, or offboarded user must be rejected**, using whatever
  check the normal login path already performs. Run that check; do not
  reimplement it.
- Do not trust `name` or `email` claims for anything but display. The local
  record is authoritative for everything that matters.

### `iss` is not automatic — verify the deployment actually sets it

The signer writes only `iat`, `exp` and `jti`. **`iss`, `aud`, `scope` and
every other claim exist only if the operator put them in the template's
`claims`.** A deployment that forgot `iss` produces tokens that your `iss`
check will reject — and if you wrote the check to skip when the claim is
absent, you have removed the check instead.

So: require `iss` to be present *and* correct. If real tokens lack it, that is
a server configuration bug to fix in `MULTICA_TASK_TOKEN_TEMPLATES`, not
something to work around here.

## Step 5 — Apply existing permissions and audit

Hand the resolved user to the same code path a browser session uses. Concretely:

- Authorization must go through the existing checks, unchanged. No new role, no
  "agent" bypass, no elevated path.
- The audit trail must record **both halves of the delegation**: the
  accountable human (`sub`) as the actor, and that an agent performed it on
  their behalf. The recommended template carries the actor claims for exactly
  this — `act_sub` / `act_name` name the executing agent and `task_id` names
  the run — and `jti` is the correlation id that joins your audit row to
  Multica's own issuance log. Store what your schema has room for; "was this
  automation?" must stay answerable with a query, or you have traded the
  service account's blind spot ("who asked?") for a new one.
  `identity.source` (if templated, commonly as `src`) additionally records
  which attribution path authorized the run.

## Step 6 — Prevent duplicate business operations

A task identity token is a **reusable access credential for one run**. The
same `jti` legitimately appears on many requests, including multiple writes.
Do not reject a request merely because that `jti` was seen before; keep it for
audit correlation or explicit token revocation.

For irreversible operations such as payments or shipments, use the system's
existing business idempotency mechanism. If it has none, accept an operation
idempotency key and atomically bind it to the authenticated user, endpoint and
request payload. A retry with the same key and payload returns the stored
result without repeating the operation; reuse with a different payload is
rejected. Different operations use different keys, even with the same JWT.
The retention period follows the business retry window, not the JWT lifetime.

Idempotency prevents accidental duplicate effects; it does not stop use of a
stolen bearer token to submit new operations. If a particular action needs
single-use authorization, issue a separate action-bound credential for it.

## Reference implementation

Language-agnostic, so translate it into whatever this system is written in.
The cache below is per process. Prewarm it at startup; during a cold-cache
refresh other requests fail closed until keys are available. Deployments with
many worker processes should use a shared cache/refresh coordinator if they
need one fleet-wide refresh limit. Review `MAX_STALE` against your revocation
requirements. The example pins the current endpoint's five-minute cache TTL:

```python
import threading
import time
import requests
from jose import jwt  # any mainstream JWT library works

JWKS_URL = "https://multica.domain.com/.well-known/jwks.json"
ALLOWED_ALGS = ["ES256"]          # pin to what your deployment signs with
EXPECTED_ISS = "multica"
EXPECTED_SCOPE = "erp"

CACHE_TTL = 300                    # matches this endpoint's max-age=300
REFRESH_COOLDOWN = 30             # applies to success AND failure
MAX_STALE = 3600                  # maximum age since last successful fetch
_cache, _cached_at = None, float("-inf")
_last_attempt = float("-inf")
_refresh_lock = threading.Lock()

def _keys(force=False):
    global _cache, _cached_at, _last_attempt
    now = time.monotonic()
    needs_refresh = force or _cache is None or now - _cached_at >= CACHE_TTL
    if (needs_refresh and now - _last_attempt >= REFRESH_COOLDOWN
            and _refresh_lock.acquire(blocking=False)):
        try:
            # Recheck after taking the lock: another request may have refreshed.
            now = time.monotonic()
            if now - _last_attempt >= REFRESH_COOLDOWN:
                _last_attempt = now
                try:
                    response = requests.get(JWKS_URL, timeout=3)
                    response.raise_for_status()
                    keys = response.json()["keys"]
                    if not isinstance(keys, list) or not all(isinstance(k, dict) for k in keys):
                        raise ValueError("invalid JWKS")
                    _cache, _cached_at = keys, time.monotonic()
                except (requests.RequestException, ValueError, KeyError):
                    pass                 # bounded stale fallback below
        finally:
            _refresh_lock.release()
    if _cache is None or time.monotonic() - _cached_at >= MAX_STALE:
        raise ValueError("JWKS unavailable")
    return _cache                        # no wait for an in-flight refresh

def _key_for(kid):
    for key in _keys():
        if key.get("kid") == kid:
            return key
    for key in _keys(force=True):          # unknown kid: the key may be new
        if key.get("kid") == kid:
            return key
    raise ValueError(f"no key for kid {kid!r}")

def authenticate(authorization_header):
    """Returns your own user object, or raises."""
    if not authorization_header.startswith("Bearer "):
        raise ValueError("missing bearer token")
    token = authorization_header[7:]

    kid = jwt.get_unverified_header(token).get("kid")
    claims = jwt.decode(
        token,
        _key_for(kid),
        algorithms=ALLOWED_ALGS,           # never read this from the token
        issuer=EXPECTED_ISS,               # must be present and correct
        # If the deployment templates "aud", pass audience=... here and drop
        # the verify_aud override — an unchecked aud is an unchecked claim.
        options={"require_exp": True, "verify_aud": False},
    )

    if claims.get("scope") != EXPECTED_SCOPE:
        raise ValueError("token is not scoped to this system")

    # sub is the full corporate email in the recommended template; adjust the
    # column if your deployment maps something else (confirm in Step 1).
    user = User.objects.filter(email=claims["sub"], is_active=True).first()
    if user is None:
        raise ValueError("no active local user for this identity")
    # Keep the delegation visible to your audit code: who acted for the user.
    request.delegation = {k: claims.get(k) for k in ("act_sub", "act_name", "task_id", "jti", "src")}
    return user                            # from here on: your existing code
```

Then wire it in as one more way to establish the current user — beside session
cookies and API keys — and let everything downstream stay exactly as it is.

Library notes: Python `PyJWT`/`python-jose`, PHP `firebase/php-jwt`
(`JWK::parseKeySet`), Node `jose` or `jwks-rsa`, Java `nimbus-jose-jwt`
(`RemoteJWKSet` handles caching and refetch for you), Go `github.com/lestrrat-go/jwx`.
All of them consume this JWKS document directly.

## Verify your work

Do not report the integration as done until each of these has actually been
run and observed:

- [ ] A valid token authenticates and the request runs as the right local user.
- [ ] An **expired** token is rejected. (Wait one out, or have the operator
      issue a template with `"ttl": "1m"`.)
- [ ] A token whose signature was altered by one character is rejected.
- [ ] A token re-signed with `alg: none` is rejected.
- [ ] A token re-signed with `HS256`, using the public key as the secret, is
      rejected. **Actually construct this one** — it is the attack that a
      missing allowlist enables, and the only way to know your allowlist works
      is to watch it refuse.
- [ ] A token whose `iss` is wrong is rejected.
- [ ] A token whose `sub` names no local user is rejected, and no account was
      created.
- [ ] A token for a disabled user is rejected.
- [ ] A token scoped to a different system is rejected.
- [ ] The audit row for an agent-performed action names the human **and**
      records that an agent acted for them (and which one, when `act_sub` /
      `act_name` are present).
- [ ] "Which actions came from agents?" is answerable with a query against
      your audit store.
- [ ] A user with limited rights gets exactly those rights — not more, not the
      old service account's.
- [ ] Many unknown `kid` values cause at most one refresh per cooldown.
- [ ] A failed refresh also observes the cooldown, including on a cold cache.
- [ ] Concurrent requests share one refresh; warm-cache reads do not wait for it.
- [ ] A rotated key is picked up after the cooldown without restarting.
- [ ] An outage permits cached keys only until the configured maximum stale age.
- [ ] Two distinct writes using the same JWT both succeed when authorized.
- [ ] Retrying one business idempotency key does not repeat its effects; reusing
      that key with a different payload is rejected.

Write these as tests in the system's own test suite. They are the regression
fence for a security boundary; a manual pass does not survive the next refactor.

## Anti-patterns

Each of these has been the actual cause of a broken integration.

| Do not | Why |
| --- | --- |
| Decode the token without verifying the signature | Anyone can craft a payload. `sub` is only meaningful after verification. |
| Read `alg` from the token to choose the verification method | This is the `none`/`HS256` attack. Pin an allowlist. |
| Auto-create a user when `sub` is unknown | Turns an authentication failure into account provisioning, from an unauthenticated endpoint. |
| Fall back to a service account when verification fails | Restores exactly the anonymous-bot problem this replaces. |
| Grant agent requests a special role or bypass | The point is that the human's existing permissions apply. |
| Log the actor as "bot" or the agent's name — or as only the human | Audit must name **both**: the accountable human as the actor, and the agent that acted for them (`act_sub` / `act_name` / `task_id`). Only the human, and agent actions are indistinguishable from their own logins; only the bot, and accountability is lost. |
| Hard-code the public key in configuration | Rotation then requires editing every system. Fetch the JWKS. |
| Cache the JWKS forever with no refetch on unknown `kid` | Rotation breaks silently at the next key change. |
| Skip `iss`/`scope` because "the signature proves it's ours" | The signature proves Multica issued it, not that it was meant for *this* system. |
| Treat a missing claim as a passing check | A deployment that forgot to template `iss` would silently disable your check. |
| Accept the token in a URL query parameter | It lands in access logs, proxies, and referrers. Header only. |
| Log the token, even at debug level | It is a live credential for its whole TTL. |
| Extend the TTL to avoid re-issuing | The token sits in a process environment for the whole run; its lifetime is its exposure window. |

## When it does not work

| Symptom | Cause |
| --- | --- |
| JWKS URL returns 404 | Task tokens are not configured on that Multica server. |
| "No key for kid" | Template has no `key_id`, so tokens carry no `kid` — match the kid-less entry. Or the key rotated and your cache is stale. |
| Signature fails, everything else looks right | Usually an algorithm mismatch, or a proxy altering the response body. Compare the token header's `alg` with your allowlist. |
| `iss` check fails on every token | The deployment did not template `iss`. Fix the server catalog; do not remove the check. |
| Token is absent from the agent's environment | Check the issuing side: an enabled template, a capable daemon, a proven human triggerer or schedule creator, intact same-human lineage, and current workspace membership are required. Webhooks without an authenticated human, audit-only attribution, and offboarded users receive no generated token. |
| Works for one person, fails for another | The `sub` mapping does not hold for every user. Check for an address that does not match the local record — or, on a local-part mapping, an email whose local part does not match the username. |
| One template never produces tokens for some users | The issuing side's `allowed_domains` excludes their email domain — intended behavior for guests/contractors. Confirm with the operator before "fixing" it. |
