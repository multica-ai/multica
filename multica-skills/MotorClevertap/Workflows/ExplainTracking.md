# ExplainTracking Workflow

**Purpose:** Explain how a MyMotor feature, journey, or funnel is tracked end-to-end in CleverTap.

## When to use

Trigger this workflow when the user asks:

- "How do we track the challan payment funnel?"
- "What events fire when a user adds a vehicle to garage?"
- "What's the activation funnel look like in CleverTap?"
- "How does the insurance expiry alert series work?"
- "What's the event sequence for OTP signup?"

The unit of answer here is a **journey** (a sequence of events), not a single event.

---

## Steps

### Step 1: Identify the journey

Map the user's question to one of the 12 canonical journeys (the same ones in `~/Downloads/telemetry/user-journeys.md`):

1. App Install → First Search (Activation)
2. RC Search
3. Challan Search
4. Challan Payment (revenue)
5. Garage Management
6. Glovebox (DigiLocker)
7. Fuel Price Lookup
8. Mobile Auth (OTP)
9. FASTag Lookup
10. Insurance Check
11. Account Deletion
12. App Lifecycle

If the question doesn't map to a known journey, ask the user to scope it (e.g. "do you mean the search funnel or the payment funnel?").

### Step 2: Pull the event sequence

For each journey there's a canonical sequence. The shortest faithful representation is:

```
event_1 (props) → event_2 (props) → event_3 (props)
```

Use the catalog (`EventDesign.md`) for the exact property list per event. Use `SKILL.md` for the high-level funnel diagrams (Challan Payment is documented inline there).

### Step 3: Flag gaps

For each step in the sequence, note whether it's tracked today or is a known gap. The most common gaps:

| Journey            | Gap                                                          |
|--------------------|--------------------------------------------------------------|
| Activation         | No event between `signup_success` and `rc_search_submit` capturing first home interaction |
| Challan Payment    | No `challan_selected`, no `challan_checkout_viewed`          |
| Challan Payment    | Webview exit events have no `transaction_id` / `vehicle_no`  |
| Challan Payment    | `payment_success_app` (backend) has no `convenience_fee`     |
| Fuel Price         | Only error path tracked — `fuel_price_success` is ORPHANED   |
| Insurance          | No success event, no provider-selected event                 |
| Account Deletion   | No success event, only error path                            |

### Step 4: Format the answer

Use this template:

```
**Journey: <name>**
**Surface(s):** mobile / web / backend

Event sequence:
1. `event_1` — <trigger>; props: a, b, c
2. `event_2` — <trigger>; props: d, e
3. `event_3` — <trigger>; props: f

Backend follow-up (if relevant): <event> fires server-side on <trigger>.

Known gaps: <list> — see SKILL.md "Revenue-critical funnel" if applicable.
```

### Step 5: Distinguish mobile vs web vs backend

- Mobile events are the unsuffixed ones (`rc_search_success`).
- Web events end in `_web` (`rc_search_success_web`).
- Backend events fire from `eventsvc` in Go; the catalog notes `(BE debug event)` where applicable.

If the user asks about a journey that spans surfaces (e.g. payment, which starts on mobile but completes on backend), call out the handoff explicitly.

---

## Refusal

If the user is asking about a journey that isn't in the canonical 12, don't invent one. Say:

> That journey isn't documented in the current CleverTap event design. The canonical 12 are <list>. If this is a new flow, it needs to be added to the tracking plan first.
