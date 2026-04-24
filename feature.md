# План: привязка Workspace/Project к физическим папкам и создание из существующей папки

## Краткий вывод по реализуемости

Функционал **реализуем**, но сейчас в продукте нет единой сущности «локальная папка»:

- `workspace` хранит удалённые репозитории (`repos` JSON), но не локальные пути.
- `project` не хранит ссылку на путь на диске.
- daemon работает от общего `WorkspacesRoot` и временных рабочих директорий по задачам, а не от пользовательских «постоянных» путей.

Т.е. нужно добавить новый слой модели данных и синхронизации, а не только поле в UI.

## Что есть сейчас (важное для дизайна)

1. **Workspace:**
   - Бэкенд сериализует `workspace` с полями `context`, `repos`, `issue_prefix` и т.д.
   - Создание workspace (`POST /api/workspaces`) принимает только `name/slug/...`, без пути.
   - Обновление workspace (`PUT /api/workspaces/{id}`) сейчас уже умеет обновлять `repos`, но не filesystem path.

2. **Project:**
   - `project` таблица и API создания/обновления не содержат поля пути.
   - Frontend-модалки/формы создания проекта не запрашивают путь.

3. **Daemon / CLI:**
   - Daemon имеет глобальный `WorkspacesRoot` (на уровне машины/профиля).
   - Для workspace daemon получает только список repo URL (`/api/daemon/workspaces/{id}/repos`).
   - Нет API контракта «workspace/project -> local path».

## Архитектурные решения (рекомендуемые)

### 1) Разделить **глобальную логическую привязку** и **машино-специфичную локальную привязку**

Критичный момент: один и тот же workspace открыт у разных пользователей/машин/OS, и «локальный абсолютный путь» не может быть универсальным серверным атрибутом.

Рекомендация:

- На сервере хранить **опциональный canonical path intent** (для self-hosted/single-machine сценариев), но не считать его единственным источником истины.
- Отдельно хранить (или вычислять) **machine-specific mapping**: `workspace_id + device_id -> local_path`.

Если сделать одно поле `workspace.local_path`, сразу появятся конфликты между устройствами и утечки приватных путей.

### 2) Для Project — путь может быть относительным к workspace

Для проекта лучше поддержать две модели:

- `project.local_path_mode = "relative"`, `project.local_path = "services/api"` (от workspace root)
- `project.local_path_mode = "absolute"`, `project.local_path = "/Users/alex/dev/other-repo"`

По умолчанию рекомендовать relative.

### 3) Создание «из папки» должно быть двухфазным

1. Выбор папки на клиенте (desktop/web + local helper).
2. Валидация и нормализация пути на стороне daemon/desktop bridge.
3. Создание сущности (workspace/project) с передачей безопасного payload в API.

Нельзя доверять пути напрямую из browser-only web-клиента (без локального bridge).

## Подводные камни

1. **Мульти-девайс и shared workspaces**
   - Разные участники имеют разные файловые системы.
   - Серверный абсолютный путь для одного пользователя невалиден для другого.

2. **Безопасность и приватность**
   - Путь может раскрывать имя пользователя/структуру системы.
   - Нужна проверка прав (кто может менять path): owner/admin.

3. **Кросс-платформенность путей**
   - Windows (`C:\...`), POSIX (`/home/...`), symlink, UNC path.
   - Нужен единый валидатор/нормализатор и правила сериализации.

4. **Существующие процессы daemon**
   - Сейчас daemon ориентирован на repo URL + рабочие директории задач.
   - Добавление постоянных путей не должно ломать GC, checkout и sandbox поведение.

5. **Web-продукт без локального доступа к ФС**
   - Для web нужна интеграция через daemon API или File System Access API (ограничено браузером/разрешениями).
   - Desktop реализуется проще через Electron dialog.

6. **Миграции и backward compatibility данных**
   - Новые поля должны быть nullable и опциональными.
   - Текущие сценарии без path должны работать как раньше.

## Предлагаемый поэтапный план реализации

## Этап 0 — Product/Domain decisions (обязательно)

- Зафиксировать точную семантику:
  - path на workspace — глобальный или device-specific?
  - path на project — absolute, relative, или оба?
  - кто может менять path?
  - нужен ли аудит изменений пути?

Deliverable: ADR/документ решения.

## Этап 1 — Data model + migrations

1. **Workspace**
   - Вариант A (минимальный): `workspace.local_path TEXT NULL`.
   - Вариант B (правильнее): новая таблица `workspace_local_binding(workspace_id, device_id, local_path, ...)`.

2. **Project**
   - Добавить поля:
     - `local_path TEXT NULL`
     - `local_path_mode TEXT CHECK (absolute|relative) NULL`

3. SQLC:
   - Обновить `server/pkg/db/queries/*.sql`.
   - Перегенерировать `server/pkg/db/generated`.

## Этап 2 — Backend API

1. **Workspace API**
   - `CreateWorkspaceRequest` + `UpdateWorkspaceRequest` расширить path-полями.
   - Валидация (пустые строки, max length, mode).

2. **Project API**
   - `CreateProjectRequest` + `UpdateProjectRequest` расширить path-полями.

3. **Создание из папки**
   - Новые endpoint-ы (пример):
     - `POST /api/workspaces/from-folder`
     - `POST /api/projects/from-folder`
   - Или флаг в существующих create endpoint-ах (`source: "folder"`).

4. Права доступа:
   - Только owner/admin для workspace path.
   - Для project path — как минимум editor/admin (по текущей RBAC модели).

## Этап 3 — Core types + API client + React Query

- Обновить `packages/core/types/workspace.ts`, `project.ts`.
- Обновить `packages/core/api/client.ts` DTO и методы create/update.
- Проверить invalidation/query keys (чтобы новое поле корректно попадало в кеш).

## Этап 4 — UI/UX (web + desktop)

1. **Workspace**
   - В форме создания workspace добавить переключатель:
     - обычное создание
     - создание из существующей папки
   - В settings добавить редактирование привязанной папки.

2. **Project**
   - В create-project modal добавить поле `Path` + mode (`relative/absolute`).
   - В project detail/settings показать и редактировать путь.

3. **Desktop**
   - Использовать native folder picker.

4. **Web**
   - Если нет daemon/bridge — либо read-only, либо manual input path (с предупреждением).

## Этап 5 — Daemon integration

- Расширить daemon API payload, чтобы daemon мог получать локальные пути.
- В task execution resolver: при наличии project/workspace path — использовать его как preferred root.
- Fallback на текущий `WorkspacesRoot` оставить как default.

## Этап 6 — Тесты

1. Backend:
   - unit/integration тесты на create/update + from-folder flow.
   - тесты валидации path (invalid/empty/long/platform-specific).

2. Frontend:
   - tests для новых полей форм, submit payload, optimistic updates.

3. Daemon:
   - tests на выбор рабочей директории и fallback.

## Этап 7 — Rollout

- Feature flag `local_folder_binding`.
- Поэтапное включение: desktop first -> web.
- Наблюдаемость: логирование ошибок валидации и количества привязок путей.

## Рекомендуемый MVP (самый практичный старт)

1. Добавить только **workspace.local_path (nullable)** + UI в Settings.
2. Для project пока добавить только `relative_path` (без absolute).
3. Добавить создание project/workspace из папки **в desktop**, а web оставить без folder picker на первом шаге.
4. Daemon: использовать путь, если задан; иначе старое поведение.

Это даст быстрый value и минимальный риск, затем расширить до device-specific mappings.

---

## Текущий статус реализации (MVP)

- Реализованы поля `local_path` для `workspace` и `project`.
- Добавлена backend-нормализация/валидация путей (absolute-only, trim/clean, reject control chars).
- Исправлена PATCH-семантика: отсутствие `local_path` в payload не затирает поле; `null` очищает значение.
- В UI добавлены и ужесточены сценарии `Create from existing folder` для workspace/project.
- Миграция local-path перенесена на `059_*` для совместимости с `origin/main` (`058` уже занят autopilot-миграцией).

Ограничение MVP: модель хранит глобальный `local_path` и рассчитана на single-machine/self-hosted сценарии.
Для multi-device требуется отдельная device-specific mapping модель.
