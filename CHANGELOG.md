# Changelog

## Unreleased

### Breaking Changes

#### `agent list` / `agent get` no longer return plaintext `custom_env` values

**Before:** `multica agent list --output json` and `multica agent get <id> --output json` returned plaintext `custom_env` values for callers with agent-owner or workspace owner/admin role.

**After:** `custom_env` values are **always masked** (`"****"`) in all list and get responses, regardless of caller role or the per-record `custom_env_redacted` flag. Only keys are returned.

**Migration:** Scripts that parsed plaintext `custom_env` from `agent list` or `agent get` output must be updated to use the new `multica agent reveal-env <id>` command (see below).

### New Features

#### `multica agent reveal-env` — audited plaintext access for `custom_env`

A dedicated command for retrieving plaintext environment variable values. Requires agent-owner or workspace owner/admin role. Every call writes a structured server-side audit log entry recording the actor, agent ID, workspace, and keys revealed.

```bash
# Reveal all keys
multica agent reveal-env <agent-id>

# Reveal specific keys
multica agent reveal-env <agent-id> --key API_KEY --key SECRET

# JSON output
multica agent reveal-env <agent-id> --output json
```

The corresponding API endpoint is `POST /api/agents/{id}/reveal-env`.

### Deprecations

- **`custom_env_redacted` per-record flag (write path):** This field on agent records no longer affects the read response shape — values are always masked in `agent list` / `agent get`. The write path still accepts the flag for one release cycle; it will be removed in a future version. Do not add new code that depends on it.
