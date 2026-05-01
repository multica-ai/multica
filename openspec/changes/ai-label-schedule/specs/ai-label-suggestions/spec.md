## ADDED Requirements

### Requirement: AI label suggestion request
The system SHALL accept a `POST /workspaces/:id/ai/label` request with a list of issue IDs (max 20) and return AI-generated label suggestions per issue. Each suggestion SHALL indicate whether the label already exists in the workspace or is new (with a suggested color).

#### Scenario: Suggestions for existing labels
- **WHEN** the AI determines an issue matches a workspace label by name
- **THEN** the suggestion SHALL include `"existing": true` and the existing `label_id`

#### Scenario: Suggestions for new labels
- **WHEN** the AI suggests a label not present in the workspace
- **THEN** the suggestion SHALL include `"existing": false`, no `label_id`, and a suggested hex `color`

#### Scenario: Batch request
- **WHEN** multiple issue IDs are provided
- **THEN** the response SHALL include a result entry per issue, each with its own suggestions list

#### Scenario: Over limit
- **WHEN** more than 20 issue IDs are provided
- **THEN** the server SHALL return `400 Bad Request`

### Requirement: Apply label suggestions
The frontend SHALL display AI label suggestions in a preview modal where the user can deselect individual suggestions before applying. Applying SHALL call existing label endpoints (`findOrCreate` + `AddIssueLabel`) — no new apply endpoint required.

#### Scenario: Preview and confirm
- **WHEN** the user selects issues and clicks "Suggest Labels"
- **THEN** a modal shows suggestions grouped by issue with checkboxes, and an "Apply" button applies only checked suggestions

#### Scenario: Apply creates missing labels
- **WHEN** a new-label suggestion is applied
- **THEN** the label is created in the workspace and attached to the issue

### Requirement: Bulk trigger from issue list
The issues list SHALL expose a "Suggest Labels" action that is available when one or more issues are selected via multi-select.

#### Scenario: Single issue trigger
- **WHEN** a single issue is selected and "Suggest Labels" is clicked
- **THEN** the suggestion modal opens with results for that issue

#### Scenario: Multi-issue trigger
- **WHEN** multiple issues are selected and "Suggest Labels" is clicked
- **THEN** the suggestion modal opens with results for all selected issues
