# Client Isolation Model & Multica Hosting-License Decision

> **Status:** DECIDED — Model B (client self-hosts; 3J bills MCP/AI layer + setup/support only)
>
> **Effective:** 2026-06-03
>
> **Owner:** Mohit Shinde (3J Technologies)
>
> **Review trigger:** Before adding any net-new paying client, or if Multica changes its license terms.

---

## 1. License Snapshot

Multica is distributed under a **modified Apache 2.0 license** (`LICENSE` at repo root, © 2025 Multica, Inc.) with two additional restrictions:

| Clause | Restriction |
|--------|-------------|
| §1a — Hosted service | You may **not** use the source code to provide a hosted service **to third parties**, or embed it in a product commercially distributed to third parties, without a commercial license from Multica. **Internal use within a single organization does not require a commercial license.** |
| §1b — Logo/brand | You may **not** remove or modify the Multica logo or copyright information in the frontend (`apps/web/`). |

**What this means for 3J:**
- Running Tracker on 3J-owned infra and letting a client's employees log in = **3J is the host delivering a service to a third party** → falls under §1a.
- Removing the Multica logo to present it as "3J Tracker" (already done in PR #1 rebrand) = **violates §1b** unless cleared by one of the four options below.

---

## 2. Options Evaluated

### Option A — Buy Multica commercial license
- **Pro:** Cleanest; enables hosting on 3J infra, allows full rebrand.
- **Con:** Multica's commercial pricing is not publicly listed (enterprise deal). For a first non-IT client the cost is unknown and likely disproportionate to deal size. Blocks demo timeline.
- **Verdict:** Defer unless client volume justifies it. Revisit when ARR target for tracker crosses ₹20L/year.

### Option B — Client self-hosts; 3J bills MCP/AI layer only ✅ CHOSEN
- **Pro:** §1a explicitly exempts *internal use within a single organization*. The client runs the tracker on **their own server**. They are not receiving it as a hosted service from 3J — they are self-hosting. The §1a restriction is not triggered.
- **Con:** Client needs a server (VPS/cloud). 3J charges for: initial deployment, `krisala-tracker-mcp` AI layer, and ongoing support — NOT for "hosting the tracker".
- **Logo:** §1b still applies — the Multica logo/copyright must remain in the frontend until we have a commercial license. **The PR #1 rebrand (changing display name/title) must NOT remove or alter the Multica copyright notice or logo asset.** See §4 below.
- **Verdict:** ADOPTED. Details in §3.

### Option C — Present as "powered by Multica" (keep logo)
- **Pro:** Zero friction on §1b.
- **Con:** Does not resolve §1a if 3J is still the host. Also "powered by Multica" is less clean for a non-IT client onboarding experience.
- **Verdict:** Partially compatible with Model B (logo must stay anyway). Not a standalone option.

### Option D — Pivot to permissive OSS base (Plane, Linear OSS)
- **Pro:** MIT/Apache-2-no-extra-conditions; full rebrand freedom.
- **Con:** Significant re-implementation effort. Plane's self-host is heavy. Loses Multica's agent-dispatch architecture which is the AI differentiator.
- **Verdict:** Only if Multica raises prices prohibitively or becomes closed-source.

---

## 3. Chosen Model — B in Detail

### What 3J does
1. **Deploys** the tracker on the **client's own infrastructure** (their cloud account, their VPS, or their on-prem server). 3J provides deployment scripts/Docker compose files.
2. **Installs and maintains** the `krisala-tracker-mcp` AI layer on the same or adjacent infra.
3. **Invoices** the client for:
   - One-time setup/deployment fee
   - Monthly AI-layer support retainer (covering `krisala-tracker-mcp`, model API costs, updates)
   - Optional: hourly/incident support for the tracker itself
4. **Does NOT** invoice for "tracker hosting" or "tracker SaaS". The tracker is open-source software running on the client's hardware.

### What 3J does NOT do
- Does NOT operate a multi-tenant SaaS where multiple clients share a single tracker instance on 3J infra.
- Does NOT position itself as "the host" of the tracker in any contract, invoice, or sales deck.
- Does NOT remove Multica branding from the frontend (see §4).

### Client data isolation
Each client gets a **dedicated deployment** — separate Docker stack, separate PostgreSQL database, separate domain/subdomain. No cross-client data sharing.

### 3J's own instance (`tracker-v1.3jtech.app`)
3J's production instance is for **3J internal use only** (single organization). This is explicitly within the §1a internal-use exemption. No external clients log in to this instance.

---

## 4. Logo / Rebrand Constraint (§1b)

The PR #1 rebrand renamed the app to "3J Tracker" at the product/navigation level. This is acceptable **only if** the underlying Multica copyright notice and logo assets in `apps/web/` are preserved.

**Checklist before any frontend deploy to a client:**
- [ ] `apps/web/` still contains the original Multica logo asset files (do not delete or replace them).
- [ ] The Multica copyright string `© 2025 Multica, Inc.` (or equivalent, as it appears in the source) is not removed from the UI footer/about page.
- [ ] Any "powered by" attribution is visible to logged-in users (footer or settings/about page is sufficient).

If a client specifically requests full white-labeling (Multica branding removed), that requires either:
- Option A (commercial license from Multica), or
- Option D (pivot to permissive OSS base).

Document that requirement in the client SOW before promising it.

---

## 5. Contract & Billing Language

### Language to USE in client contracts / SOW

> "3J Technologies will deploy and configure the open-source 3J Tracker platform on [Client]'s infrastructure. The tracker software is derived from Multica (multica.ai), an open-source project licensed under a modified Apache 2.0 license. [Client] owns and operates the tracker instance on their own servers. 3J's services under this agreement cover deployment, the AI integration layer (krisala-tracker-mcp), and ongoing technical support."

### Language to AVOID in client contracts / SOW

- "3J will host your tracker" — implies hosted service, triggers §1a.
- "3J Tracker SaaS" — SaaS framing triggers §1a.
- "3J's proprietary tracker platform" — misrepresents open-source base.
- "White-labeled Multica" — implies brand removal, triggers §1b unless licensed.

### Invoice line items

| Line item | OK | Not OK |
|-----------|-----|--------|
| Deployment & Setup | ✅ | |
| AI Layer (krisala-tracker-mcp) | ✅ | |
| Monthly support retainer | ✅ | |
| Tracker hosting fee | | ❌ |
| Tracker SaaS subscription | | ❌ |

---

## 6. Residual Gaps & Follow-ups

| Gap | Owner | Timeline |
|-----|-------|----------|
| §1b audit: verify Multica logo/copyright not removed in PR #1 rebrand before first client deploy | Engineer doing deploy | Before client onboarding |
| Get Multica commercial license quote for future white-label deals | Mohit | When deal > ₹20L/yr tracker ARR |
| Add "About / Powered by Multica" link in footer or settings page of deployed instance | Frontend | Pre-client deploy sprint |
| Formalize SOW template with approved contract language from §5 | Mohit / legal | Before contract signing |
| Re-evaluate if Multica changes license (watch upstream LICENSE commits) | Mohit | Quarterly / on upstream merge |

---

## 7. Decision Log

| Date | Decision | Rationale |
|------|----------|-----------|
| 2026-06-03 | Adopt Model B (client self-host) | Avoids §1a; no commercial license cost for first client; 3J charges for AI layer not hosting; keeps demo timeline intact |
| 2026-06-03 | Defer Option A (commercial license) | No public pricing; unknown cost; defer until deal volume justifies |
| 2026-06-03 | Reject Option D (pivot to Plane) | Loses agent-dispatch AI architecture; too costly for demo sprint |
