# Kort forklaring: hvorfor TECH-3077 kunne ramme gammel Claude-session

Konklusion: TECH-3077 fik ikke kun en ny besked med en begrænset issue-snapshot. Claude blev også startet med den gamle Claude-session, fordi koden genbruger tidligere sessioner på samme agent og issue, medmindre opgaven er markeret som frisk kørsel.

Det betyder: 40k-grænsen beskytter kun den tekstpakke, Multica selv lægger ind i den nye besked. Den beskytter ikke mod gammel historik/cache inde i Claude-sessionen.

## Hvad sker der i koden?

1. Multica bygger først en ny prompt (besked til agenten) i en fælles prompt-builder.

Bevis: `server/internal/daemon/prompt.go:19`

```go
func BuildPrompt(task Task, provider string) string {
```

2. Hvis workspace har snapshot slået til, kan serveren lægge issue + seneste tråd ind i prompten.

Bevis: `server/internal/handler/daemon_cost_snapshot_cerebro.go:57`

```go
func (h *Handler) applySnapshotSaving(ctx context.Context, resp *AgentTaskResponse, issue db.Issue, triggerCommentID, taskID pgtype.UUID) {
```

3. 40k-grænsen ligger kun på den snapshot-tekst.

Bevis: `server/internal/handler/daemon_cost_snapshot_cerebro.go:44-48`

```go
snapshotMaxChars = 40_000
```

4. Serveren finder samtidig en tidligere session for samme agent og issue, hvis opgaven ikke er tvunget frisk.

Bevis: `server/internal/handler/daemon.go:1735-1743`

```go
// Look up the prior session for this (agent, issue) pair so the daemon
// can resume the Claude Code conversation context.
if !task.ForceFreshSession {
```

5. Hvis den tidligere session er fra samme runtime, sendes den til daemonen som `PriorSessionID`.

Bevis: `server/internal/handler/daemon.go:1753-1755`

```go
if prior.RuntimeID == task.RuntimeID {
    resp.PriorSessionID = prior.SessionID.String
}
```

6. Daemonen sender den videre til runtime-kaldet.

Bevis: `server/internal/daemon/daemon.go:3131-3137`

```go
execOpts := agent.ExecOptions{
    ResumeSessionID: task.PriorSessionID,
}
```

7. Claude-runtime laver det om til `claude --resume <session-id>`.

Bevis: `server/pkg/agent/claude.go:596-598`

```go
if opts.ResumeSessionID != "" {
    args = append(args, "--resume", opts.ResumeSessionID)
}
```

## Lille matrix

| Spørgsmål | Svar |
| --- | --- |
| Er prompten forskellig per udbyder? | Delvist. Selve opgaveprompten bygges fælles, men runtime-filer og resume-flag er forskellige. |
| Er interactive anderledes end print/default? | Nej for prompten. Interactive spejler output til terminalen, men bruger samme prompt- og sessionvej. |
| Er issue-runs anderledes end chat-runs? | Ja. Issues bruger issue/snapshot/tråd. Chat bruger chat-beskeder, chat-historik og chat-attachments. |
| Hvad dækker 40k-cap’en? | Kun inline issue-snapshotten, ikke gammel Claude-session, cache, skills, tools eller chat-historik. |
| Hvorfor ramte TECH-3077 gammel cache? | Fordi Claude blev startet med `--resume` på den tidligere session. |

## Hvad bliver lagt oveni?

Multica kan lægge flere ting oveni før runtime starter:

- Agentens faste instruktioner og skills.
- Workspace context (fælles arbejdsplads-besked).
- Repo- og projektressourcer.
- Runtime tools/MCP-konfiguration.
- Trigger-kommentaren ved kommentar-runs.
- Issue snapshot eller besked om at hente samlet issue context.
- Chat-beskeder og chat-attachments ved chat-runs.
- Tidligere provider-session via `PriorSessionID`.

Issue-attachments bliver ikke automatisk lagt ind i selve prompten. Chat-attachments bliver nævnt med ID og filnavn, så agenten kan hente dem med `multica attachment download <id>`.

## Anbefaling

Der er et doc-gap og muligvis en produktbeslutning der mangler:

1. Dokumentér tydeligt at 40k snapshot kun begrænser Multicas nye snapshot-tekst.
2. Opret en opfølgende ændring: når snapshot bruges som “frisk/capped context”, skal Multica enten slå Claude resume fra eller have en indstilling som hedder fx “start frisk session ved snapshot”.

Indtil da skal man bruge fresh-session/manual rerun, når en Claude-session har ramt 1M-context eller anden cache-forgiftning.
