## ADDED Requirements

### Requirement: Workspace AI configuration
The workspace SHALL support an AI configuration object stored in `workspace.settings` under the `"ai"` key. This configuration includes provider, API key, model, base URL, and label rules. Workspace admins (members) SHALL be able to read and update the AI configuration via API.

#### Scenario: Save AI configuration
- **WHEN** an authenticated workspace member sends `POST /workspaces/:id/ai/settings` with a valid configuration body
- **THEN** the server updates `workspace.settings["ai"]` and returns the saved configuration (API key redacted to last 4 chars)

#### Scenario: Read AI configuration
- **WHEN** an authenticated workspace member sends `GET /workspaces/:id/ai/settings`
- **THEN** the server returns the current AI configuration with the API key masked (e.g. `sk-...xxxx`)

#### Scenario: Missing API key fallback
- **WHEN** workspace AI settings have no `api_key` set
- **THEN** the server SHALL fall back to the `DEEPSEEK_API_KEY` environment variable

#### Scenario: No API key available
- **WHEN** neither workspace settings nor the env var provides an API key
- **THEN** AI endpoints SHALL return `402 Payment Required` with a message indicating AI is not configured

### Requirement: Label rules configuration
The AI configuration SHALL include a `label_rules` field — an ordered list of natural-language rule strings (e.g. `"frontend related issues → frontend label"`). Rules are passed as part of the LLM system prompt when generating label suggestions.

#### Scenario: Rules applied to suggestions
- **WHEN** label rules are configured and `POST /workspaces/:id/ai/label` is called
- **THEN** the LLM receives the rules as part of its system prompt and suggestions reflect them

#### Scenario: Empty rules
- **WHEN** `label_rules` is empty or absent
- **THEN** the LLM suggests labels based on issue content and existing workspace labels without custom rules
