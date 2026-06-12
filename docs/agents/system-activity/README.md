# System Activity — fælles system-trigger

"System Activity" er platformens mekanisme til at vække en agent på et issue på et fremtidigt tidspunkt. Det er IKKE:
- En kommentar- eller mention-trigger
- En autopilot
- En sprint recurring task
- Den gamle "recurring wakeup" (fjernet i Phase 1)

## Arkitektur (Phase 1)

```
Agent kald schedule_wakeup (trigger_type=time, fire_at=T+N min)
  └─ Server opretter cerebro_agent_wakeup (state=pending)
       └─ Sweeper (hvert 30s) finder due wakeups (state=claimed)
            └─ Service.Dispatch():
                 1. CancelAgentTasksByIssueAndAgent (ryd hængende tasks)
                 2. CreateComment (type=wakeup, synlig i tråden)
                 3. EnqueueTaskForMention (agent modtager wakeup)
                 4. IncrementWakeupPostpones (tæl consecutive)
                    └─ Hvis >= 3: CreateInboxItem til issue-ejer + reset
```

## Subdokumenter

- [wakeup.md](wakeup.md) — detaljer om wakeup-mekanismen
- [release-notes.md](release-notes.md) — hvad der ændrede sig i Phase 1
