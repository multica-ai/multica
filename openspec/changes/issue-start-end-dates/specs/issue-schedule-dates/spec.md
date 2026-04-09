## ADDED Requirements

### Requirement: Issues store optional schedule dates
The system SHALL allow each issue to store optional `start_date` and `end_date` values, and SHALL return those values in issue create, read, update, and list responses whenever they are present.

#### Scenario: Creating an issue with a schedule window
- **WHEN** a client creates an issue with both `start_date` and `end_date`
- **THEN** the created issue response includes the same `start_date` and `end_date` values

#### Scenario: Creating an issue without schedule dates
- **WHEN** a client creates an issue without `start_date` and `end_date`
- **THEN** the issue is created successfully with both fields unset

#### Scenario: Listing issues with stored schedule dates
- **WHEN** an issue has a stored `start_date` or `end_date`
- **THEN** issue list and single-issue responses include the stored values for those fields

### Requirement: Issues support independent schedule date updates and clearing
The system SHALL allow clients to set, change, or clear `start_date` and `end_date` independently on an existing issue, and SHALL treat explicit `null` as a request to clear that field.

#### Scenario: Updating only the start date
- **WHEN** a client updates an existing issue with a new `start_date` and does not send `end_date`
- **THEN** the issue stores the new `start_date` and keeps the previous `end_date` unchanged

#### Scenario: Clearing only the end date
- **WHEN** a client updates an existing issue with `end_date` set to `null`
- **THEN** the issue clears `end_date` and leaves `start_date` unchanged

#### Scenario: Clearing both schedule dates
- **WHEN** a client explicitly sets both `start_date` and `end_date` to `null`
- **THEN** the issue clears both schedule date fields

### Requirement: Issues reject an inverted schedule window
The system SHALL reject any create or update request whose effective schedule would make `start_date` later than `end_date` when both values are present.

#### Scenario: Rejecting an invalid window on create
- **WHEN** a client creates an issue with `start_date` later than `end_date`
- **THEN** the request is rejected with a validation error

#### Scenario: Rejecting an invalid window on update
- **WHEN** an issue already has `end_date` set and a client updates `start_date` to a later value than that `end_date`
- **THEN** the request is rejected with a validation error

### Requirement: Issue workflows expose schedule dates in both frontend apps
The system SHALL let users view and edit `start_date` and `end_date` in issue creation and issue detail workflows in both `apps/web` and `apps/workspace`.

#### Scenario: Setting schedule dates while creating an issue
- **WHEN** a user sets `start_date` and `end_date` in the create-issue flow
- **THEN** the created issue persists both values and shows them in issue detail properties

#### Scenario: Editing schedule dates in issue detail
- **WHEN** a user changes either `start_date` or `end_date` from the issue detail properties
- **THEN** the issue detail view reflects the updated values after the save completes

#### Scenario: Clearing a schedule date in issue detail
- **WHEN** a user clears `start_date` or `end_date` from the issue detail view
- **THEN** the cleared field is no longer shown as set in the issue properties UI

### Requirement: Issue updates expose explicit schedule-date change metadata
The system SHALL mark `start_date` and `end_date` changes explicitly in issue update events so downstream activity and notification consumers can identify which schedule field changed and what the previous value was.

#### Scenario: Start date change emits previous value metadata
- **WHEN** a client changes `start_date` on an issue
- **THEN** the issue update event identifies that `start_date` changed and includes the previous `start_date` value

#### Scenario: End date clear emits previous value metadata
- **WHEN** a client clears `end_date` on an issue
- **THEN** the issue update event identifies that `end_date` changed and includes the previous `end_date` value