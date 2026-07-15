# Mini-app workflows

A workflow is an immutable versioned JSON document attached to an app version. It has one trigger and an ordered list of steps.

Supported triggers:

- `manual`: a signed-in person starts it.
- `chat`: an agent starts it with `show_app_view` and an optional chat or issue target.
- `schedule`: Hatchet evaluates a five-field cron expression.
- `webhook`: a caller uses the workflow's unguessable token.
- `data_event`: Registry sends a resource event with an idempotency key.

Supported steps are `registry.read`, `registry.write`, `filter`, and `view.show_and_wait`. Registry steps mint a fresh personal key and carry the run UUID as the Registry trace ID. A view step stores a durable request, marks the run `waiting`, publishes an interactive card, and changes the same run back to `running` after a request-bound submission.

Hatchet owns dispatch and retries; the Multica backend owns identities, keys, state, and audit data. Never put credentials or a human identity in workflow JSON.

