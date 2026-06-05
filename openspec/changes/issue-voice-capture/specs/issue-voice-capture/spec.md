# Issue Voice Capture — Spec

## ADDED Requirements

### Requirement: Issue creation supports non-streaming voice transcription

The system SHALL let an authenticated user record audio from the create-issue experience, submit the completed recording for non-streaming server-side transcription, and review the transcript before it mutates the issue draft.

#### Scenario: Recording and transcribing audio

- **WHEN** a user starts voice capture from the create-issue flow
- **AND** the browser grants microphone access
- **AND** the user stops recording
- **THEN** the system submits the completed audio recording to the server-side transcription endpoint
- **AND** shows a transcribing state until a transcript or error is returned

#### Scenario: Reviewing transcript before insertion

- **WHEN** the transcription endpoint returns transcript text
- **THEN** the system shows the transcript in a review state
- **AND** does not mutate the issue title or description until the user confirms insertion

#### Scenario: Discarding transcript

- **WHEN** the user discards a transcript review
- **THEN** the system keeps the existing issue draft unchanged
- **AND** keeps manual issue creation available

### Requirement: Transcript text maps into the issue draft deterministically

The system SHALL apply deterministic mapping rules when inserting voice transcript text into the create-issue draft.

#### Scenario: Title is empty

- **WHEN** the user confirms transcript insertion while the title field is empty
- **AND** the transcript contains a usable first sentence or first line
- **THEN** the system uses that first sentence or first line as a title candidate
- **AND** appends the full transcript into the issue description draft
- **AND** lets the user edit both fields before creating the issue

#### Scenario: Title already exists

- **WHEN** the user confirms transcript insertion while the title field already contains text
- **THEN** the system keeps the existing title unchanged
- **AND** appends the full transcript into the issue description draft

#### Scenario: Description already exists

- **WHEN** the issue description already contains content
- **AND** the user confirms transcript insertion
- **THEN** the system appends the transcript after a blank line
- **AND** preserves the existing description content

#### Scenario: Transcript is too short for title

- **WHEN** the title is empty
- **AND** the transcript is too short to form a usable title candidate
- **THEN** the system leaves the title empty
- **AND** still appends the transcript into the description draft
- **AND** tells the user that the title needs manual confirmation

### Requirement: Server-side transcription uses a provider abstraction

The system SHALL model audio transcription as a dedicated provider capability separate from text chat-completion AI settings.

#### Scenario: Cloudflare provider is configured

- **WHEN** the server is configured with `TRANSCRIPTION_PROVIDER=cloudflare`
- **AND** the required Cloudflare credentials are present
- **THEN** `POST /api/transcriptions` uses the Cloudflare transcription provider
- **AND** returns a normalized transcription response to the client

#### Scenario: Transcription provider is not configured

- **WHEN** the server does not have a transcription provider configured
- **THEN** `POST /api/transcriptions` fails with a provider-not-configured error
- **AND** the manual create-issue flow remains usable

#### Scenario: Workspace has text AI configured but no transcription provider

- **WHEN** a workspace has text AI settings configured for labels or scheduling
- **AND** the server has no transcription provider configured
- **THEN** the system does not assume audio transcription is available
- **AND** does not reuse text chat-completion credentials for audio transcription

### Requirement: Issue creation can preserve the original voice recording as an attachment

The system SHALL allow users to keep the original recording and SHALL upload that recording as an issue attachment only after issue creation succeeds.

#### Scenario: Keeping the original recording

- **WHEN** a user confirms transcript insertion
- **AND** chooses to keep the original recording
- **AND** successfully creates the issue
- **THEN** the system uploads the recorded audio after issue creation succeeds
- **AND** links the uploaded audio to the created issue as an attachment

#### Scenario: Not keeping the original recording

- **WHEN** a user confirms transcript insertion
- **AND** chooses not to keep the original recording
- **THEN** the system creates the issue from the transcript-enhanced draft
- **AND** does not upload the audio recording as an attachment

#### Scenario: Attachment upload fails after issue creation

- **WHEN** issue creation succeeds
- **AND** the follow-up audio attachment upload fails
- **THEN** the issue remains created
- **AND** the user is told that the transcript was saved but the recording was not preserved

### Requirement: Voice capture degrades without breaking manual issue creation

The system SHALL keep manual issue creation available when recording, transcription, or optional attachment preservation fails.

#### Scenario: Browser does not support recording

- **WHEN** the client lacks required recording capabilities
- **THEN** the voice control is hidden or disabled with an explanation
- **AND** title, description, and normal issue creation remain available

#### Scenario: Microphone permission is denied

- **WHEN** the user denies microphone permission
- **THEN** the system shows a permission error
- **AND** returns the voice flow to a recoverable state
- **AND** keeps manual issue creation available

#### Scenario: Transcription fails

- **WHEN** recording succeeds
- **AND** server-side transcription fails
- **THEN** the system shows a transcription error
- **AND** allows the user to retry or discard the recording
- **AND** keeps manual issue creation available

### Requirement: Streaming transcription remains a future compatible extension

The system SHALL keep the Phase 1 non-streaming API and UI structure compatible with a future streaming provider without implementing streaming in Phase 1.

#### Scenario: Future streaming provider is added

- **WHEN** a future implementation adds streaming transcription
- **THEN** it uses a separate session or WebSocket contract
- **AND** does not overload `POST /api/transcriptions` with chunk or partial transcript semantics

#### Scenario: Frontend provider changes

- **WHEN** the backend provider changes from Cloudflare to another provider
- **THEN** the create-issue UI keeps using the same recording, review, insertion, and optional attachment flow
- **AND** does not require provider-specific UI states

## Acceptance Criteria

- AC1: A logged-in workspace member can open create issue, start recording, stop recording, and see a non-streaming transcription progress state.
- AC2: When Cloudflare transcription is configured and succeeds, the user can review transcript text before any title or description mutation happens.
- AC3: Confirming transcript insertion with an empty title fills a title candidate from the first sentence or first line and appends the full transcript to description.
- AC4: Confirming transcript insertion with an existing title preserves that title and appends the full transcript to description.
- AC5: Confirming transcript insertion with an existing description appends the transcript after a blank line without deleting existing content.
- AC6: Discarding transcript review leaves title and description unchanged.
- AC7: If the browser cannot record, microphone permission is denied, transcription is disabled, or transcription fails, manual issue creation remains usable.
- AC8: The backend exposes `POST /api/transcriptions` as an authenticated, workspace-scoped, non-streaming multipart endpoint.
- AC9: The backend transcription path uses a `TranscriptionProvider` abstraction and does not reuse `AISettings`, `buildLLMClient`, or `DEEPSEEK_API_KEY`.
- AC10: With `TRANSCRIPTION_PROVIDER=cloudflare` and valid Cloudflare credentials, the backend calls the Cloudflare provider and returns a normalized transcript response.
- AC11: Without a configured transcription provider, the transcription endpoint returns a provider-not-configured error without breaking create issue.
- AC12: Raw audio is uploaded as an issue attachment only after issue creation succeeds and only when the user chooses to keep the recording.
- AC13: If optional audio attachment upload fails after issue creation, the issue remains created and the user sees a distinct warning that the recording was not preserved.
- AC14: Phase 1 does not implement WebSocket streaming or partial transcript chunk semantics in `POST /api/transcriptions`.
- AC15: The create issue UI does not contain Cloudflare- or Doubao-specific control flow; provider-specific behavior stays behind the backend transcription API.
