# Mini-app workflows

Workflows are not created, edited, or tested from the Apps catalog or app detail screen. Apps is a pure app surface powered by the app SDK; the separate **Workflows** product owns workflow authoring and runs. This document only describes the legacy backend attachment contract for integrations that still refer to an app version.

The CLI follows the same boundary: author current workflows with `multica workflow create --file workflow.json`, which uses `POST /api/cerebro/workflows`. The legacy `multica app workflow` command and `/api/cerebro/app-workflows` route are not workflow-authoring surfaces, and a legacy app-workflow document must not be sent unchanged to the current Workflows route.

A workflow is an immutable versioned JSON document attached to an app version. It has one trigger and an ordered list of steps.

Supported triggers:

- `manual`: a signed-in person starts it.
- `chat`: an agent starts it with `show_app_view` and an optional chat or issue target.
- `schedule`: Hatchet evaluates a five-field cron expression.
- `webhook`: a caller uses the workflow's unguessable token.
- `data_event`: Registry sends a resource event with an idempotency key.

Supported steps are `registry.read`, `registry.write`, `filter`, and `view.show_and_wait`. Registry steps mint a fresh personal key and carry the run UUID as the Registry trace ID. A view step stores a durable request, marks the run `waiting`, publishes an interactive card, and changes the same run back to `running` after a request-bound submission.

Hatchet owns dispatch and retries; the Multica backend owns identities, keys, state, and audit data. Never put credentials or a human identity in workflow JSON.
