# Task Identity Tokens

Let an agent act on your internal systems **as the person who asked**, instead
of as a shared service account.

At claim time the server signs a short-lived JWT naming the human who
authorized the run, and the daemon injects it into the agent process. The agent presents
that token to your ERP, wiki, or admin backend, which sees a request from that
person and applies the permissions it already has for them.

Off unless configured. An unconfigured deployment behaves exactly as before.

- [Why this exists](#why-this-exists)
- [How it works](#how-it-works)
- [Configuration](#configuration)
- [Enabling tokens for an agent](#enabling-tokens-for-an-agent)
- [Who gets an identity](#who-gets-an-identity)
- [Daemon support](#daemon-support)
- [Verifying tokens in your system](#verifying-tokens-in-your-system)
- [Security boundaries](#security-boundaries)
- [Troubleshooting](#troubleshooting)

## Why this exists

Self-hosted teams run agents next to systems that already have a permission
model built over years and an audit trail that names real people. Connecting an
agent to one of those systems normally means issuing it a service account, and
that breaks two things at once:

- **Permissions collapse to one level.** Every user's agent acts with the same
  fixed identity. A junior engineer's agent can reach whatever the service
  account can. The existing model stops applying exactly where automation
  starts touching things.
- **Audit goes anonymous.** The internal system logs "the bot did it". Who
  actually asked is lost — usually the one thing an audit needs.

So the practical answer becomes "don't connect agents to the systems that
matter", and self-hosting loses much of its point.

Task identity tokens remove that trade-off. **Nothing about your permission
model changes.** Your system keeps doing what it always did — check who this
is, apply their rights, log their name. It simply receives a real person
instead of a shared bot.

## How it works

```
   Alice assigns an issue to an agent
             │
             ▼
 ┌───────────────────────────────────────────────────────────────────┐
 │ Multica server                                                    │
 │                                                                   │
 │  1. Claim: which human authorized this run?                       │
 │     Read originator_user_id — the authorization column — not      │
 │     accountable_user_id, which is an audit label that names       │
 │     someone even when nobody authorized anything.                 │
 │                                                                   │
 │       a human authorized it → continue                            │
 │       nobody did            → issue nothing, run proceeds without │
 │                                                                   │
 │  2. For each template this agent has enabled:                     │
 │     interpolate {{identity.*}} into the claims, sign a JWT        │
 │     (asymmetric, TTL <= 24h, unique jti)                          │
 └───────────────────────────────┬───────────────────────────────────┘
                                 │  claim response
                                 ▼
 ┌───────────────────────────────────────────────────────────────────┐
 │ Daemon, on the runtime machine                                    │
 │                                                                   │
 │  3. Inject into the agent process environment:                    │
 │       BOT_TOKEN_ERP=eyJhbGciOiJFUzI1NiIsImtpZCI6ImVycC0yMDI2...   │
 └───────────────────────────────┬───────────────────────────────────┘
                                 ▼
 ┌───────────────────────────────────────────────────────────────────┐
 │ Agent process                                                     │
 │                                                                   │
 │  4. curl -H "Authorization: Bearer $BOT_TOKEN_ERP" \              │
 │          https://erp.domain.com/api/orders                          │
 └───────────────────────────────┬───────────────────────────────────┘
                                 ▼
 ┌───────────────────────────────────────────────────────────────────┐
 │ Your internal system — unchanged permission model                 │
 │                                                                   │
 │  5. Fetch the public keys, once, then cache: ──────────┐          │
 │       GET $MULTICA_PUBLIC_URL/.well-known/jwks.json    │          │
 │                                                        │          │
 │  6. Verify the signature using the key whose "kid"     │          │
 │     matches the token header; check exp, then iss      │          │
 │                                                        │          │
 │  7. sub = "alice@domain.com" → look up YOUR OWN user     │          │
 │                                                        │          │
 │  8. Apply the permissions Alice already has            │          │
 │                                                        │          │
 │  9. Write the audit row naming Alice                   │          │
 └────────────────────────────────────────────────────────┼──────────┘
                                                          │
        ┌─────────────────────────────────────────────────┘
        │  Multica serves the public half of the signing key here.
        ▼  No auth: verifiers hold no Multica credentials.
   GET /.well-known/jwks.json  →  { "keys": [ { "kty": "EC", ... } ] }
```

Steps 5–9 happen in **your** code. Everything Multica does ends at step 3;
from there it has stated, verifiably, who is asking — and nothing more. It
gains no authority over your system, and your system decides what that identity
may do, exactly as it does for a browser session.

Be precise about what this is: **delegated user impersonation**, in the sense
of [RFC 8693](https://www.rfc-editor.org/rfc/rfc8693) token exchange — the
token's `sub` is the accountable human, and the recommended `act_sub` /
`act_name` claims name the agent actually executing, so a receiving system can
record "agent X, on behalf of Alice" rather than mistaking the action for
Alice's own. It is *not* workload attestation in the GitHub Actions OIDC sense;
those tokens deliberately never name a user. Anyone reviewing this feature
should evaluate it as delegation, with the trust obligations that carries.

## Configuration

Two environment variables on the server. Setting one without the other is a
startup error — a deployment that meant to enable this should be told it is not
enabled, rather than left silently off.

| Variable | Required | Purpose |
| --- | --- | --- |
| `MULTICA_TASK_TOKEN_PRIVATE_KEY` | yes | PEM signing key. PKCS#8 (`PRIVATE KEY`), SEC 1 EC (`EC PRIVATE KEY`), or PKCS#1 RSA (`RSA PRIVATE KEY`). |
| `MULTICA_TASK_TOKEN_TEMPLATES` | yes | JSON array of templates — what may be signed, and into which variable. |
| `MULTICA_TASK_TOKEN_MANIFEST_ENV` | no | Name of an extra variable carrying a JSON description of the systems actually issued for this run. |

Generate a key:

```bash
openssl ecparam -genkey -name prime256v1 -noout \
  | openssl pkcs8 -topk8 -nocrypt -out task-token.key
```

### The template catalog

```json
[
  {
    "id": "erp",
    "label": "ERP",
    "description": "erp.domain.com — orders, inventory",
    "env": "BOT_TOKEN_ERP",
    "algorithm": "ES256",
    "key_id": "erp-2026",
    "ttl": "8h",
    "allowed_domains": ["domain.com"],
    "claims": {
      "iss": "multica",
      "aud": "erp.domain.com",
      "scope": "erp",
      "sub": "{{identity.email}}",
      "name": "{{identity.name}}",
      "src": "{{identity.source}}",
      "act_sub": "{{agent.id}}",
      "act_name": "{{agent.name}}",
      "task_id": "{{task.id}}"
    },
    "manifest": { "base_url": "https://erp.domain.com", "name": "ERP" }
  }
]
```

| Field | Required | Notes |
| --- | --- | --- |
| `id` | yes | Stable identifier. This is what an agent enables. |
| `label` | yes | Shown in the UI. |
| `description` | no | Shown in the UI. |
| `env` | yes | Variable the token is injected as. Must be `^[A-Z][A-Z0-9_]*$` and must not collide with a name the daemon owns (`HOME`, `PATH`, `CODEX_HOME`, …). |
| `algorithm` | no | `ES256` (default), `ES384`, `RS256`, `RS384`. Asymmetric only — a verifier must never need the signing secret. Must match your key type. |
| `key_id` | no | Emitted as the JWT header `kid` and published in the JWK Set. Omit only if you never intend to rotate. |
| `ttl` | no | Go duration. Default `1h`, maximum `24h`. |
| `claims` | yes | The claim set, with interpolation. `iat`, `exp` and `jti` are written by the signer and cannot be templated. |
| `allowed_domains` | no | Domains the accountable human's email must belong to for this template to sign, compared case-insensitively. An identity outside the list gets no token from this template. Strongly recommended for any template whose `sub` drops the domain. |
| `manifest` | no | Opaque JSON, copied through verbatim. See below. |

**The whole catalog is validated at startup.** An unknown variable, a bad env
name, a TTL over the maximum, a reserved claim, two templates sharing one `env`
(the second would silently replace the first's token), a manifest variable
named after a template's `env`, or an `algorithm` the configured key cannot
produce (`RS256` with an EC key) — the server refuses to boot rather than run
on half a configuration and fail at 3am.

### Interpolation variables

| Variable | Value |
| --- | --- |
| `{{identity.email}}` | `alice@domain.com` |
| `{{identity.email_local}}` | `alice` — local part, lowercased, `+tag` stripped |
| `{{identity.name}}` | `Alice Chen` |
| `{{identity.id}}` | Multica user UUID |
| `{{identity.source}}` | Attribution source that resolved this human |
| `{{workspace.id}}` | Workspace UUID |
| `{{workspace.slug}}` | Workspace slug |
| `{{agent.id}}` | UUID of the agent executing the run |
| `{{agent.name}}` | Name of the agent executing the run |
| `{{task.id}}` | UUID of the task being run |

**The whole trust chain roots in the email address Multica stores for the
accountable human.** A verified corporate email for every member who can reach
these agents is a hard prerequisite — if workspace emails are self-asserted or
external, the identity this feature attests is only as trustworthy as they are.

Default `sub` to `{{identity.email}}`. The full address is unambiguous;
`{{identity.email_local}}` drops the domain, so `alice@domain.com` and
`alice@external.com` both sign as `alice` — in a workspace with guests or
external collaborators that is a privilege-escalation path, not a convenience.
Use `email_local` only when the receiving system genuinely keys on the local
part, and then **always pair it with `allowed_domains`** so only your own
domain can be signed by that template.

Put `act_sub` / `act_name` / `task_id` (the `{{agent.*}}` and `{{task.*}}`
variables) in every template. They are the flat-claim form of RFC 8693's `act`:
without them, the receiving system's audit row is indistinguishable from the
person having logged in themselves, and "which changes came from agents?"
becomes unanswerable — the other half of the audit story this feature exists
to fix.

### The manifest variable

`MULTICA_TASK_TOKEN_MANIFEST_ENV` names a variable that receives a JSON array
of the `manifest` blocks of the templates **actually issued** for this run:

```json
[{ "base_url": "https://erp.domain.com", "name": "ERP" }]
```

A tool's view of "which systems can I reach" is then derived from the
credentials it actually holds, rather than configured separately and left to
drift. Nothing in the block is interpreted by Multica.

## Enabling tokens for an agent

**Settings → Agents →** pick the agent **→ Identity Tokens.** The tab sits beside Environment and Skills, lists the catalog and lets
an operator enable a subset per agent; it is hidden entirely when the feature
is unconfigured.

The catalog lives only in server configuration, so the UI can pick from what an
operator allowed but can never define what may be signed. Changing the list
requires workspace owner/admin or the agent's owner — the same authorization as
agent environment variables — and every change lands in the activity log.

## Who gets an identity

A token is signed only for a run **a member asked for** — one whose delegation
chain begins with a person's own action. Multica records the authorizing human
in `originator_user_id`; that column, followed back to the root of the chain,
decides. The audit label beside it never does:

| Source | Signed | Meaning |
| --- | --- | --- |
| `direct_human` | yes | A person acted |
| `delegation` | only if the chain root is `direct_human` | An agent hop copied the human from the run above it |
| `comment_source` | only if the chain root is `direct_human` | Same, traced through a comment |
| `trigger_owner` | **no** | An autopilot trigger fired on its own |
| `rule_owner` | **no** | Same, with the rule's publisher as the audit label |
| `owner_fallback` | **no** | Nobody authorized this; the owner is a guess |
| `backfill` | **no** | Reconstructed after the fact |
| `unattributed` | **no** | Unknown |

The autopilot rows are the ones worth understanding. Inside Multica an armed
schedule or webhook *does* run with its creator's authorization (MUL-6951):
arming the trigger is treated like a deferred "run now", so the run's
`originator_user_id` names that person and every agent it delegates to copies
them down the chain. This feature still refuses to sign there. Letting an
automation invoke an agent under someone's rights is a bounded grant inside the
product; handing an unattended 3am run that person's full permissions in *your*
systems, on input they never saw, is not the same grant — and the person is
not watching. So the gate walks a delegation chain back to its root and signs
only when that root is a member's own action. A chain whose root cannot be
proven (a deleted ancestor) is refused rather than assumed.

The person must also still be a member of the workspace when the run is
claimed. Attribution is decided when the run is queued, signing when it is
claimed, and a queue can wait days for an offline runtime — no credential is
minted in the name of someone offboarded in between.

A run that is refused simply gets no token, proceeds normally, and whatever
needed the token sees an ordinary "unauthorized" — never a task failure. If you
want a scheduled run to reach an internal system, give that system its own
service account for that job: the point of this feature is that a *person's*
authority is only ever lent to work that person asked for.

**Every issuance is audited in-product.** Each time tokens are minted for a
run, an `agent_task_tokens_issued` row lands in the activity log, tied to the
run's issue, recording the accountable human, the agent, the task, and the
`jti` and expiry of every token signed. The `jti` is the correlation handle: a
receiving system that logs it can be joined against Multica's activity log
end-to-end. If the audit row cannot be written, the tokens are withheld — a
credential minted in someone's name is never invisible to them.

## Daemon support

Tokens are only signed for a daemon that advertises it can inject them
(`task-identity-tokens-v1`). A daemon older than that support silently ignores
the field, so issuing for one would write an "a credential was minted for
Alice" audit row for a token that never reached a process — worse than no row
at all. The server therefore checks first and signs nothing.

**Upgrade order: daemons first, then enable the feature.** If you enable it
while a runtime is still on an older daemon, its runs simply get no tokens and
nothing is logged as issued; upgrading that daemon starts issuance with no
further server change. Runs already in flight keep whatever environment they
started with.

## Verifying tokens in your system

The public keys are served, unauthenticated, at:

```
GET https://multica.domain.com/.well-known/jwks.json
```

```json
{
  "keys": [
    {
      "kty": "EC", "crv": "P-256", "use": "sig", "alg": "ES256",
      "kid": "erp-2026",
      "x": "f83OJ3D2xF1Bg8vub9tLe1gHMzV76e8Tus9uPHvRVEU",
      "y": "x_FEzRu9m36HLN_tue659LNpXW6pCyStikYjKIWI5a0"
    }
  ]
}
```

Any mainstream JWT library consumes this directly. The endpoint returns **404**
when the feature is not configured, so "not configured" is distinguishable from
"configured but no keys".

If your internal systems sit behind an API gateway, verification can be
zero-code: most gateways (Kong, APISIX, nginx with a JWT module, the cloud API
gateways) accept an issuer/JWKS pair directly in bearer-JWT validation mode.
Configure the expected `iss` from your templates and this endpoint as the
`jwks_uri`, and the gateway performs steps that would otherwise be middleware.

**For the integration itself — including a step-by-step checklist, worked
verification code, and the mistakes that actually bite — see
[TASK_IDENTITY_TOKENS_AI.md](TASK_IDENTITY_TOKENS_AI.md).** That document is
written to be handed to an AI agent working inside your legacy codebase.

## Security boundaries

**This feature is designed for self-hosted deployments**, where the people
operating Multica and the people operating the internal systems are the same
organization. Do not configure an internal system to trust a Multica server
your organization does not control: whoever holds that server's signing key
can mint any configured identity.

**Know what a server compromise buys.** The signing key can sign any identity
in the catalog for any configured system. Enabling this feature therefore
escalates a Multica server compromise from "Multica's own data is exposed" to
"write to every integrated system as anyone" — cross-trust-domain lateral
movement, the same shape of risk as compromising an SSO provider. Weigh that
before enabling it, protect the key accordingly (the catalog and key live only
in server configuration, never in the database or the UI), and scope templates
to the narrowest systems that need them.

**The blast radius inverts relative to a service account.** A service account
is usually narrowed on purpose — often to read-only — while this feature hands
the agent the requester's full permissions in the receiving system: the more
senior the requester, the more the token can do. Agents also routinely process
untrusted input (issue bodies, comments, fetched pages), so prompt injection
executes with a real person's authority, not a deliberately-narrowed bot's.
Deploy accordingly: start with systems that are read-only or low-consequence,
use per-system `scope` claims and have receiving systems enforce them, and
prefer teaching the *receiving* system to restrict what token-authenticated
sessions may do (e.g. read-only at first) over relying on the person's full
rights being safe to automate. "No configuration needed, it just inherits the
person" is the riskiest way to run this.

**Tokens are not refreshed.** The TTL is fixed at claim time and there is no
renewal channel: a task that outlives its token loses access mid-run, and the
failure surfaces to the agent as an ordinary 401 — expiry is not distinguished.
Long-running tasks are not supported beyond the configured TTL; pick a TTL
that covers your typical run length (capped at 24h) rather than reaching for
the cap by default, and treat a mid-run 401 after hours of work as probable
expiry.

**Multica's own credentials are untouched.** The agent's access to Multica's
API remains owner-scoped. What is signed here is a credential for a *different
trust domain*: systems outside Multica that opt into accepting it. Multica
never gains authority over the target system; it only states who is asking.

**The token lives in the process environment for the whole run**, so its
lifetime is its exposure window. That is why the TTL is capped at 24h — prefer
hours. Anything that can read the agent's environment can use the token until
it expires.

**Key ids appear in the public JWK Set.** Do not name them after anything
confidential.

**Rotation currently has a gap.** One private key is configured, so replacing
it means restarting the server, and tokens signed with the old key stop
verifying as soon as the old public key leaves the JWK Set. To rotate without
breaking in-flight runs: schedule the change, wait out the longest configured
TTL, then swap. Publishing overlapping keys is not supported yet.

**The endpoint is public.** It discloses the public keys and the key ids, and
the fact that this deployment issues task tokens. It discloses nothing else,
and never any private component.

## Troubleshooting

| Symptom | Cause |
| --- | --- |
| Server refuses to start, complains about the catalog | A template is invalid. The message names the entry and the field; the catalog is all-or-nothing on purpose. |
| Server refuses to start: "catalog is configured but the private key is empty" | Only one of the two variables is set. |
| Server refuses to start: "needs an EC private key" / "needs a P-256 key" | A template's `algorithm` does not match the configured key. Change one of the two; the check exists so this fails at boot instead of on every task. |
| Server refuses to start: "env … is already used by template" | Two templates share one `env`, or the manifest variable is named after a template's. Each token needs its own variable. |
| `/.well-known/jwks.json` returns 404 | The feature is not configured on this server. |
| Identity Tokens tab is missing | Same — the tab is hidden when the catalog is unset. |
| No token in the agent's environment | The agent has none enabled, no human authorized the run (an autopilot trigger, for instance), the daemon is older than the injection support, or the identity's email domain is outside the template's `allowed_domains`. The server logs the reason with the task id. Runs that did receive tokens carry an `agent_task_tokens_issued` row in the activity log; a run without that row was issued nothing. |
| The variable holds a different value than expected | An agent's own `custom_env` wins over an injected token, by design. |
| Verifier reports "no matching key" | The template has no `key_id`, so its tokens carry no `kid`. Either configure one, or have the verifier fall back to the kid-less entry the set publishes for exactly this case. |
| Verifier reports a malformed EC coordinate | Not from this server — `x` and `y` are padded to the curve size. Check for a proxy rewriting the response. |
