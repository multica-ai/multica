# patch.md — правки aito1-tracker поверх upstream multica

Чек-лист всех модификаций, которые отличают наш форк от upstream'а `multica-ai/multica`. Использовать при синхронизации с upstream'ом, чтобы не потерять.

---

## Workflow обновления с upstream

### Безопасный merge (по умолчанию)

```bash
cd ~/Documents/Projects/aito1-tracker
git remote add upstream https://github.com/multica-ai/multica.git 2>/dev/null  # один раз
git fetch upstream
git checkout main
git merge upstream/main
# при конфликтах — разрешить, опираясь на этот документ
go test ./server/pkg/agent/... -count=1   # проверить, что наши тесты прошли
git push origin main
```

### Альтернатива — rebase (если хочешь линейную историю)

```bash
git fetch upstream
git rebase upstream/main
# наши коммиты применятся поверх upstream/main
go test ./server/pkg/agent/... -count=1
git push --force-with-lease origin main
```

⚠️ `--force-with-lease` нужен после rebase, и он **переписывает чужую историю на GitHub** — после этого все, у кого есть локальный клон, должны сделать `git fetch && git reset --hard origin/main`.

### Чего нельзя делать

- `git reset --hard upstream/main` — это снесёт наши коммиты вообще.
- `git pull` без `--rebase` или явного merge на конфликтном файле может молча перезаписать наш код.
- Запускать установщик AITO1 без проверки, что клон содержит **оба** наших коммита (см. ниже).

---

## Проверка целостности

После любого pull/merge/rebase:

```bash
git log --oneline | grep -E "fix\(agent\): switch managed|fix\(agent\): support managed"
```

Должно показать **две** строки:
- `27ece86c fix(agent): switch managed permission mode from dontAsk to acceptEdits`
- `4008d298 fix(agent): support managed permission policies in claude backend`

(хеши после rebase будут другие, но название коммитов сохранится.)

И подтверждение по содержимому:
```bash
grep -c "acceptEdits" server/pkg/agent/claude.go    # ожидаем ≥ 2
grep -c "control_request" server/pkg/agent/claude.go # ожидаем ≥ 1 (case в loop)
grep -c "Keep stdin open" server/pkg/agent/claude.go # ожидаем 1 (наш комментарий)
```

---

## Список патчей

### Патч 1 — `handleControlRequest` подключён + stdin pipe держится открытым

**Файл:** `server/pkg/agent/claude.go`
**Коммит:** `4008d298` (исходный fix)
**Зачем:** под Jamf-managed Claude Code в режиме `bypassPermissions` / `auto` происходит **silent downgrade** до `default`, и Claude шлёт каждое использование инструмента через stream-json `control_request`. В upstream `handleControlRequest` уже написан и протестирован (`TestClaudeHandleControlRequestAutoApproves`), но **не подключён** к event-loop, плюс `stdin` закрывался сразу после prompt'а — отвечать было физически некуда.

**Что изменено:**

1. Убран ранний `closeStdin()` сразу после `writeClaudeInput`. Замена — explanatory комментарий «Keep stdin open». Найти место по контексту:
   ```go
   if err := writeClaudeInput(stdin, prompt); err != nil { ... }
   closeStdin()                                  // ← было это
   b.cfg.Logger.Info("claude started", ...)
   ```
   Заменить `closeStdin()` на:
   ```go
   // Keep stdin open: under managed permission policies (Jamf, etc.) the CLI
   // downgrades bypassPermissions/auto to default and emits stream-json
   // control_request messages for every tool use. handleControlRequest writes
   // the auto-allow control_response back through stdin, so the pipe must stay
   // open until "result" is observed (or the run is cancelled).
   ```

2. Cancel-goroutine закрывает stdin рядом с stdout:
   ```go
   // было:
   go func() { <-runCtx.Done(); _ = stdout.Close() }()

   // стало:
   go func() {
       <-runCtx.Done()
       _ = stdout.Close()
       closeStdin()
   }()
   ```

3. В switch на `msg.Type` добавить case (после `case "log"`):
   ```go
   case "control_request":
       b.handleControlRequest(msg, stdin)
   ```

**Тест:** `claude_test.go::TestClaudeHandleControlRequestAutoApproves` уже есть в upstream — после патча он по-прежнему проходит, и плюс рабочий E2E под managed policy.

---

### Патч 2 — `--permission-mode` = `acceptEdits`

**Файл:** `server/pkg/agent/claude.go`
**Коммит:** `27ece86c`
**Зачем:** под managed policy `bypassPermissions` и `auto` молча даунгрейдятся до `default`. Из доступных только `acceptEdits` и `dontAsk` сохраняют себя; `dontAsk` — **auto-deny** (всё что не в allowlist'е → отказ), не подходит. `acceptEdits` оставляет Edit/Write автоматическими, остальное гонит через `control_request`, который мы авто-апрувим патчем 1.

**Что изменено:**

В `buildClaudeArgs`:
```go
// было:
"--permission-mode", "bypassPermissions",

// стало:
"--permission-mode", "acceptEdits",
```

В `claudeBlockedArgs` обновить комментарий рядом с `--permission-mode`:
```go
"--permission-mode": blockedWithValue,  // acceptEdits + handleControlRequest auto-allow under managed policies
```

---

### Патч 3 — тест `TestBuildClaudeArgsIncludesStrictMCPConfig` ожидает `acceptEdits`

**Файл:** `server/pkg/agent/claude_test.go`
**Коммит:** `27ece86c`

```go
// было:
"--permission-mode", "bypassPermissions",

// стало:
"--permission-mode", "acceptEdits",
```

---

### Патч 4 — тест `TestClaudeExecuteSurfacesStderrWhenChildExitsEarly` (fake claude читает 1 строку)

**Файл:** `server/pkg/agent/claude_test.go`
**Коммит:** `4008d298`
**Зачем:** в патче 1 stdin не закрывается сразу. Старый fake script `cat >/dev/null` дренировал stdin до EOF — без EOF теперь висит до timeout. Заменили на `head -n 1` — читает ровно одну строку (наш prompt) и выходит, симулируя реалистично.

**Что изменено:**
```go
// было:
script := "#!/bin/sh\n" +
    "cat >/dev/null\n" +
    "echo \"FATAL ERROR: V8 abort: assertion failed\" >&2\n" +
    "exit 3\n"

// стало:
script := "#!/bin/sh\n" +
    "head -n 1 >/dev/null\n" +
    "echo \"FATAL ERROR: V8 abort: assertion failed\" >&2\n" +
    "exit 3\n"
```

И обновлён комментарий выше про обоснование (см. коммит).

---

## Если конфликт при merge/rebase

`server/pkg/agent/claude.go` — самый горячий файл (upstream активно его дорабатывает). Шаблон resolution:

| Конфликт | Что делать |
|---|---|
| Upstream поменял `buildClaudeArgs` (новый флаг, новый порядок) | Принять upstream-структуру, переставить `"--permission-mode", "acceptEdits"` обратно туда, где было `bypassPermissions`. |
| Upstream поменял event-loop (новые `case`'ы) | Принять upstream-кейсы, добавить наш `case "control_request"` рядом с `case "log"`. |
| Upstream начал слать `closeStdin()` где-то внутри loop'а сам | Удалить наш `closeStdin()` из cancel-горутины (чтобы не было двойного close), оставить случай где он закрывается единожды. |
| Upstream добавил sandbox-bypass через stream-json (новый `subtype` в `control_request`) | Расширить `handleControlRequest` (см. permission-management.md, сценарий C про логирование `control_request_raw`). |

После любого resolve:
```bash
go build ./server/... && go test ./server/pkg/agent/... -count=1
```

---

## Связанные правки **вне** этого репо (для полноты картины)

Эти правки лежат в других репо/файлах, но без них наш форк работает не полностью. Они описаны отдельно — здесь только указатели:

- **`~/.claude/settings.json permissions.allow`** — список Bash/MCP-правил для localhost-allowlist'а. Описано в [permission-management.md](https://github.yandex-team.ru/...) (вне arc, лежит в `aito1` репозитории).
- **`~/.claude/skills/wiki-patched/scripts/wiki-cli.sh`** — `curl -sf` → `curl -sSf` (локальный патч, описан в `permission-management.md`).
- **`~/secrets.env`** — `ELIZA_TOKEN` копией `AITO1_API_KEY` для работы `private-llm.sh`.
- **`/Users/wwax/arcadia/taxi/ai/aito1/install/phases/{40_multica,50_multica_daemon}.sh`** + `install/templates/config.env.template` — параметризация `AITO1_MULTICA_GIT_REPO`/`AITO1_MULTICA_GIT_REF` + сборка `cmd/multica` локально вместо brew tap. Закоммичено в arc (PR https://a.yandex-team.ru/review/13274199).
