# Wakeup — teknisk reference

## Databasetabel: `cerebro_agent_wakeup`

| Kolonne | Type | Beskrivelse |
|---|---|---|
| `state` | text | `pending`, `claimed`, `dispatched`, `failed`, `cancelled` |
| `trigger_type` | text | `time`, `issue_status`, `github_ci` (recurring fjernet i Phase 1) |
| `fire_at` | timestamptz | Hvornår wakeup'en skal affyres (required for `time`) |
| `consecutive_postpones` | int | Antal successive dispatches uden agent-afslutning (ny i Phase 1) |
| `interval_seconds` | int8 | Kun used af legacy recurring — ikke eksponeret i MCP efter Phase 1 |

## Backend-konstanter (Phase 1)

```go
const (
    WakeupMinIntervalMinutes      = 15  // minimum fire_at afstand + postpone-delay
    WakeupMaxConsecutivePostpones = 3   // maks dispatches i træk → inbox-notifikation
)
```

## Håndhævede regler ved Create

1. `fire_at` skal være mindst `WakeupMinIntervalMinutes` (15 min) frem i tid.
2. Der må ikke allerede eksistere en pending wakeup for samme `agent_id + issue_id` oprettet inden for de seneste 15 minutter.

## Dispatch-flow (Phase 1)

```
1. CancelAgentTasksByIssueAndAgent → annuller evt. hængende tasks
2. CreateComment (type="wakeup") → synlig kommentar i issuet
3. EnqueueTaskForMention → agent enqueues ny task
4. MarkWakeupDispatched / RescheduleRecurringWakeup
5. [goroutine] IncrementWakeupPostpones
   └─ count >= 3 → CreateInboxItem (type="wakeup_loop") + ResetWakeupPostpones
```

## MCP-tools (tilgængelige for agenter)

```
schedule_wakeup(issue_id, prompt, trigger_type, [fire_at], [watch_issue_id], [watch_status])
list_wakeups([agent_id], [state], [limit])
cancel_wakeup(id)
```

**trigger_type values:** `time` | `issue_status` | `github_ci`

`recurring` og `interval_seconds` er FJERNET fra MCP-skemaet i Phase 1.

## Inbox-notifikation ved postpone-loop

Når en agent har modtaget det samme wakeup 3 gange i træk (consecutive_postpones >= 3), sendes:
- `CreateInboxItem` til issue-ejer (assignee hvis member, ellers creator)
- `Type: "wakeup_loop"`, `Severity: "action_required"`
- `Details:` JSON med wakeup_id, issue_id, consecutive_count, prompt, max_postpones
- Counter resettes til 0 efter notifikation
