---
name: multica-working-on-issues
description: "Superseded by the multica-platform skill — load that instead. This file only records where the Multica issue contracts moved to."
user-invocable: false
allowed-tools: Bash(multica *)
---

# Moved into `multica-platform`

Multica's platform contracts are now one skill. Everything this skill used to
carry — PR linking vs close intent, reading a linked PR's real state, metadata
write discipline, custom properties, status side effects, sub-issues and stages,
and finding who else is running — lives in:

```text
multica-platform  →  references/issues.md
```

Load the `multica-platform` skill and open that file. Its routing table also
names the reference for every other platform domain: mentions, agents, squads,
autopilots, projects, runtimes, and skill import.

Nothing was dropped in the move — the contracts were reorganized, not shortened.

You are seeing this redirect because the Multica app on this machine is older
than the server it is talking to, so its task brief still refers to the previous
skill name. Updating the app removes this extra hop.
