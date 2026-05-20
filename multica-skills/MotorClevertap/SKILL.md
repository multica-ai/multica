---
name: MotorClevertap
description: CleverTap event design reference for the MyMotor mobile + web app (Flutter + Go backend). Catalogs every CleverTap event, its module, properties, trigger, and current implementation status across mobile, web, and backend. USE WHEN asked about clevertap, ct events, mymotor analytics, event properties, event naming, tracking plan, event payload, what does <event_name> log, how is <feature> tracked, push notification triggers, segment definition, retention alerts, payment funnel events, garage events, rc search events, challan events, glovebox events.
---

# MotorClevertap — MyMotor's CleverTap Event Design

Canonical reference for **every CleverTap event** wired into the MyMotor product (mobile + web + backend). The full catalog lives in `EventDesign.md` — 156 events across 95 modules, each with trigger description and property table. This file is the convention guide and lookup index.

If the question is "what does `<event_name>` log?", "what properties go on `<event>`?", or "how do we track `<feature>`?" — this skill is the source of truth.

## Workflow Routing

| Workflow | Trigger | File |
|----------|---------|------|
| **LookupEvent** | "what does `<event>` log", "props on `<event>`", "when does `<event>` fire", "is `<event>` tracked on mobile/web/backend" | `Workflows/LookupEvent.md` |

| **ExplainTracking** | "how do we track `<feature>`", "what's the `<funnel>` look like", "event sequence for `<journey>`" | `Workflows/ExplainTracking.md` |

## Examples

**Example 1: Single-event lookup**
```
User: "What properties does challan_init_payment_success carry?"
→ Invokes LookupEvent workflow
→ Greps EventDesign.md for `challan_init_payment_success`
→ Returns: module = Challan Payment - Mobile; trigger = on payment init API call; custom props = challan_nos, trace_id, vehicle_no, challan_count
→ Flags the known gap: total_amount and convenience_fee are NOT on this event yet (tracking-plan delta P0)
```

**Example 2: Journey explanation**
```
User: "Walk me through the challan payment funnel events"
→ Invokes ExplainTracking workflow
→ Pulls Journey 4 (Challan Payment) sequence from SKILL.md + EventDesign.md
→ Returns canonical sequence: challan_search_success → challan_init_payment_success → challan_webview_exit_success → payment_success_app (backend)
→ Calls out the 3 known gaps (no challan_selected, no challan_checkout_viewed, no convenience_fee on payment_success_app)
```

**Example 3: Cross-surface clarification**
```
User: "How does rc_search_success differ between mobile and web?"
→ Invokes LookupEvent workflow twice (rc_search_success and rc_search_success_web)
→ Returns both event payloads side-by-side from EventDesign.md
→ Notes which surface is the source of truth for funnels (mobile primary, ~571K/month)
```

## What MyMotor is

- B2C mobile app (Flutter primary, web secondary) for Indian vehicle services
- **Primary value actions:** RC Search, Challan Search, Challan Payment (the revenue event)
- **Secondary features:** Garage (up to 5 vehicles), Glovebox (DigiLocker docs), FASTag, Fuel Prices, Insurance, Custom Data, Export RC, Reminders
- Identity: Firebase UID; anonymous via `device_id` header
- ~571K RC searches / ~301K challan searches / ~228K installs / ~28K payment inits per month

## CleverTap in the stack

| Surface  | SDK / Provider                          | What's tracked here                                          |
|----------|-----------------------------------------|--------------------------------------------------------------|
| Mobile   | `clevertap_plugin` 3.8.1 (Flutter)      | All product events (fans out alongside Firebase + Meta SDK)  |
| Backend  | `providers/clevertap/` (Go)             | Server-side payment / refund / push-trigger events           |
| Web      | CleverTap Web SDK                       | Web-only RC / Challan / Auth events (suffixed `_web`)        |

**Mobile call routing.** All tracking flows through `AnalyticsService.currInstance?.logEvent(eventName, parameters)` in `initialiser_provider.dart` — single call, three destinations (Firebase + CleverTap + Meta SDK if prod). The one legacy exception is `logFirebaseEvent()` in `mobile_auth/data/api_provider.dart` (Firebase only).

**Backend call routing.** Events flow through `eventsvc`; `TrackEvent()` fans out to Mixpanel + CleverTap + Firebase.

**Identity.** Mobile calls `initialiseUser(firebaseUser)` on Google/Apple sign-in or cached session; user props `name`, `email`, `mobile`, `email_verified`, `garage_vehicle_count`. `setPhoneNumber()` after OTP verify. `resetUserId()` on logout (Meta SDK only — Firebase UID persists for cross-session linking).

**Error handling.** All `logEvent` calls are fire-and-forget; analytics failures never affect the UI. Backend errors are logged to `provider_logs` and don't affect API responses.

## Event naming convention

Every event is `snake_case` and follows `module_feature_state` where state is one of `initiate` / `success` / `error`.

- `challan_payment_success` — module=`challan`, feature=`payment`, state=`success`
- `rc_search_initiate` — module=`rc`, feature=`search`, state=`initiate`
- Web variants are suffixed `_web`: `rc_search_success_web`, `auth_initiate_web`
- Backend debug events that never reach the user are tagged in the catalog as `(BE debug event)` — keep them in dashboards but exclude from product funnels

**Trace correlation.** Every API-backed event carries `trace_id` — same value as the `X-Zoop-Trace-ID` response header. Use it to join client events to backend logs and to backend-fired CleverTap events.

**Event types.** Three buckets per the canonical doc:

- **Analytical** — funnel / retention / cohort signals (most events)
- **Debug** — engineering-only, suffixed `(BE debug event)` or `(FE debug event)` in the catalog
- **Campaign** — events explicitly designed as CleverTap journey triggers (e.g. expiry alerts, app_uninstalled)

## Universal properties (CT defaults)

Every event ships these alongside its custom props — they are **not** repeated in the catalog because they are implicit:

- `app_version`, `build_number`, `platform` (android/ios/web)
- `device_pid`, `cp_user_pid`
- `trace_id` for any API-backed event

## Global / cross-cutting events

These four fire across every screen in the app and are the highest-volume:

| Event           | Required props                                                                 | Why it matters                                  |
|-----------------|--------------------------------------------------------------------------------|-------------------------------------------------|
| `screen_view`   | `destination_screen`, `source_screen`                                          | Navigation graph + funnel step counting         |
| `modal_view`    | `modal_name`, `source_screen`                                                  | Modal exposure (e.g. add-to-garage prompt)      |
| `popup_view`    | `id`, `popup_name`                                                             | Promo / feedback / detail-sheet exposures       |
| `button_click`  | `button_name`, `screen_name` + contextual (`reg_no`, `challan_nos`, etc.)      | CTA-level instrumentation                       |

Web parallel: `page_view`, `button_click_web`.

## Revenue-critical funnel (Challan Payment)

The payment funnel is the single most important set of events. The current state has known gaps; the target state closes them.

**Current (mobile):**

```
challan_search_success
  → [GAP: no challan_selected event]
  → [GAP: no challan_checkout_viewed event]
  → challan_init_payment_success  (no amount / fee props)
  → challan_webview_exit_success / _failure  (no transaction_id / vehicle_no)
  → payment_success_app  (backend; no convenience_fee)
```

**Target additions** (see `EventDesign.md` for the full payload):

- `challan_selected` (NEW) — fires when user picks challans on the search-result screen. Props: count, total amount, source
- `challan_checkout_viewed` (NEW) — fires on checkout screen. Props: amount, convenience_fee, vehicle_no
- Enrich `challan_init_payment_success` with `total_amount`, `convenience_fee`
- Enrich `challan_webview_exit_success` with `transaction_id`, `vehicle_no`
- Enrich `challan_webview_exit_failure` with `vehicle_no`, `exit_reason` (user_cancelled / timeout / error)
- Enrich `payment_success_app` (backend) with `convenience_fee` for ROAS calculation

## Retention alerts (campaign-triggered)

These events power CleverTap journeys for re-engagement. They're fired by backend cron based on garage data:

| Series                       | Variants                                       | Trigger                                        |
|------------------------------|------------------------------------------------|------------------------------------------------|
| Insurance expiry             | `_alert_60d`, `_alert_30d`, `_alert_1d`        | Days before insurance policy expiry            |
| Insurance expired            | `insurance_expired_alert`                      | Post-expiry follow-up                          |
| PUC expiry                   | `_alert_30d`, `_alert_1d`                      | Days before PUC certificate expiry             |
| PUC expired                  | `puc_expired_alert`                            | Post-expiry follow-up                          |
| Challan pending              | (see catalog under retention)                  | New pending challan detected in garage refresh |

All carry full vehicle context (`reg_no`, `vehicle_id`, expiry date) so journeys can deep-link to the right screen.

## User traits (identify, not events)

PII lives in `identify()` calls only — never in event properties. Current traits: `name`, `email`, `mobile`, `email_verified`, `garage_vehicle_count`. Target additions per the tracking plan: `user_status` (anonymous/signed_up/activated), `auth_provider`, `signup_date`, `is_internal`, `total_rc_searches`, `total_challan_searches`, `total_payments`, `total_payment_amount`, `first_search_date`, `first_payment_date`, `last_active_date`, `platform`, `city`, `state`, `app_version`.

`is_internal=true` accounts must be filtered out at the tracking layer so internal QA traffic doesn't pollute funnels.

## How to use this skill

1. **Quote the exact event name in backticks** (`rc_search_success`, not "RC search event"). Constants are exact strings.
2. **Always remind that defaults are implicit** — the catalog only lists custom props on top of CT defaults.
3. **Flag known gaps.** If asked about a funnel step that isn't tracked yet (challan selection, checkout view, payment confirmation), say so — don't fabricate an event.
4. **Distinguish mobile vs web vs backend.** Bare `challan_search_success` is mobile; `_web` suffix is web; backend events are noted explicitly.
5. **Don't hallucinate properties.** If a property isn't in `EventDesign.md`, it isn't on the event.

## Quick Reference

**Catalog file:** `EventDesign.md` — 156 events grouped by 95 modules, each with trigger + property table

**Event naming:** `module_feature_state` snake_case (e.g. `challan_payment_success`); web variants suffixed `_web`

**Implicit props on every event:** `app_version`, `build_number`, `platform`, `device_pid`, `cp_user_pid`, `trace_id` (API events)

**Top-volume events:** `screen_view`, `button_click`, `modal_view`, `popup_view`

**Revenue funnel:** `challan_search_success` → `challan_init_payment_success` → `challan_webview_exit_*` → `payment_success_app` (with known gaps — see above)

**Retention triggers:** Insurance/PUC expiry alerts at 60d/30d/1d + post-expiry; challan pending alerts

## Files in this skill

- `SKILL.md` — this file (conventions + routing + lookup index)
- `EventDesign.md` — full 156-event catalog grouped by module, with trigger and property tables
- `Workflows/LookupEvent.md` — SOP for answering single-event questions
- `Workflows/ExplainTracking.md` — SOP for explaining a feature / funnel / journey
