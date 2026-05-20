# LookupEvent Workflow

**Purpose:** Answer a question about a specific CleverTap event in MyMotor.

## When to use

Trigger this workflow when the user asks any of:

- "What does `<event_name>` log?"
- "What properties go on `<event_name>`?"
- "When does `<event_name>` fire?"
- "Is `<event_name>` tracked on mobile / web / backend?"
- "Show me the payload for `<event_name>`."

The user may write the event name in any casing or with a friendly description (e.g. "the RC search success event"). Normalize to the canonical `snake_case` name first.

---

## Steps

### Step 1: Normalize the event name

Convert whatever the user wrote into the canonical `snake_case` form. Common patterns:

| User phrasing                                | Canonical event             |
|----------------------------------------------|-----------------------------|
| "RC search success" / "rc search succeed"    | `rc_search_success`         |
| "challan payment init" / "init payment"      | `challan_init_payment_success` (mobile) or `_web` |
| "add to garage" / "garage add"               | `add_vehicle_garage_success`|
| "insurance 30 day alert"                     | `insurance_expiry_alert_30d`|

Web variants always end in `_web`. Backend debug events are tagged `(BE debug event)`.

### Step 2: Open the catalog

```
EventDesign.md
```

Grep for the exact event name (with backticks around the name preserves heading boundaries):

```bash
grep -n "\`<event_name>\`" EventDesign.md
```

The catalog is grouped by module; each event has:

- `### \`event_name\`` heading
- **Trigger:** line — when the event fires
- Property table — name + type for each custom property

### Step 3: Compose the answer

When describing an event in your reply, always include:

1. **The exact event name in backticks.** Never paraphrase to "RC search event" — the constants are exact strings.
2. **The module** (from the catalog `## <Module>` heading above the event).
3. **The trigger description** verbatim from the catalog.
4. **Custom properties** as a bulleted list of `name` (type).
5. **The implicit defaults reminder:** every event also carries `app_version`, `build_number`, `platform`, `device_pid`, `cp_user_pid`, and (for API-backed events) `trace_id` — these are not in the catalog but always present.

If the event is `(BE debug event)` or `(FE debug event)`, flag it: "this is a debug event — not part of product funnels; keep it out of campaign segments."

### Step 4: Flag gaps if relevant

If the user is asking about a step in a funnel that has a known gap, surface it. The canonical gaps live in `SKILL.md` under "Revenue-critical funnel" — the headline gaps are:

- **No `challan_selected` event** between `challan_search_success` and checkout
- **No `challan_checkout_viewed` event** on the checkout screen
- **No `total_amount` / `convenience_fee`** on `challan_init_payment_success`
- **No `transaction_id` / `vehicle_no`** on `challan_webview_exit_success` / `_failure`
- **No `convenience_fee`** on backend `payment_success_app`

If the event the user asked about is adjacent to one of these gaps, mention it so they know the funnel is incomplete.

---

## Refusal

If the event name truly isn't in `EventDesign.md`, say so directly:

> `<event_name>` isn't in the current CleverTap event design for MyMotor. If it should be added, propose it for the next tracking-plan revision.

Do **not** fabricate properties for an event that isn't catalogued.
