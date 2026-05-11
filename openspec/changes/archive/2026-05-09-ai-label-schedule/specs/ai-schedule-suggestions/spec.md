## ADDED Requirements

### Requirement: AI schedule suggestion request
The system SHALL accept a `POST /workspaces/:id/ai/schedule` request with a list of issue IDs (max 20) and return AI-generated `start_date`/`end_date` suggestions per issue. The LLM SHALL consider issue priority, existing date assignments, and `blocks`/`blocked_by` dependency relationships when producing suggestions.

#### Scenario: Suggestions include reasoning
- **WHEN** the AI produces a schedule suggestion for an issue
- **THEN** each suggestion SHALL include a `reason` string explaining the placement

#### Scenario: Dependency ordering respected
- **WHEN** issue A blocks issue B
- **THEN** the suggested `start_date` for B SHALL be on or after the suggested `end_date` for A

#### Scenario: Existing dates preserved
- **WHEN** an issue already has `start_date` and `end_date` set
- **THEN** that issue SHALL be included in context but the response SHALL only include issues that were requested

#### Scenario: Over limit
- **WHEN** more than 20 issue IDs are provided
- **THEN** the server SHALL return `400 Bad Request`

### Requirement: Apply schedule suggestions
The frontend SHALL display AI schedule suggestions in a preview modal where the user can deselect individual issue entries before applying. Applying SHALL call the existing issue update endpoint with `start_date` and `end_date`.

#### Scenario: Preview and confirm
- **WHEN** the user clicks "Schedule with AI"
- **THEN** a modal shows a table of issue / start / end / reason with checkboxes, and an "Apply" button applies only checked rows

#### Scenario: Partial apply
- **WHEN** the user unchecks some rows and clicks "Apply"
- **THEN** only the checked issues are updated; unchecked issues are unchanged

### Requirement: Schedule trigger from issue list
The issues list and project view SHALL expose a "Schedule with AI" action available when one or more issues are selected.

#### Scenario: Multi-issue trigger
- **WHEN** one or more issues are selected and "Schedule with AI" is clicked
- **THEN** the schedule suggestion modal opens for the selected issues
