## ADDED Requirements

### Requirement: Uploaded files use effective content metadata for preview and download behavior
The system SHALL determine upload content metadata on the server, applying extension-aware overrides for known sniffing failures, and SHALL store uploaded objects with inline or attachment disposition according to whether the file type is previewable.

#### Scenario: Uploading an SVG keeps it renderable
- **WHEN** a user uploads an SVG file through the workspace upload flow
- **THEN** the stored upload uses `image/svg+xml` as its effective content type
- **AND** clients can render the uploaded file as an inline image

#### Scenario: Uploading a non-previewable document defaults to download behavior
- **WHEN** a user uploads a non-media file such as a CSV, archive, or source file
- **THEN** the stored upload uses download-oriented disposition metadata rather than forcing inline browser preview

### Requirement: Description embeds stay separate from explicit issue attachments
The system SHALL let issue descriptions embed uploaded file URLs without causing those uploads to appear in issue attachment surfaces, while comment and reply uploads SHALL remain linked to issue or comment attachment context.

#### Scenario: Uploading a file into an issue description
- **WHEN** a user uploads a file from the issue description editor
- **THEN** the editor receives a usable uploaded file URL for embedding in markdown
- **AND** the upload does not appear in the issue attachment list for that issue solely because it was embedded in the description

#### Scenario: Uploading a file into a comment or reply
- **WHEN** a user uploads a file from a comment or reply composer
- **THEN** the upload remains linked to the issue or comment attachment context
- **AND** attachment discovery for conversation files continues to work

### Requirement: Issue list responses omit description-heavy detail content
The system SHALL keep single-issue reads fully detailed while returning list-oriented issue responses without the stored rich-text description body.

#### Scenario: Listing issues with stored descriptions
- **WHEN** a client requests an issue list for board, backlog, today, upcoming, or general list usage
- **THEN** each issue response still includes the summary fields needed for list rendering
- **AND** `description` is returned as `null` instead of the full stored markdown body

#### Scenario: Reading a single issue after list optimization
- **WHEN** a client requests a single issue detail view
- **THEN** the response still includes the full stored `description` content for editing and display