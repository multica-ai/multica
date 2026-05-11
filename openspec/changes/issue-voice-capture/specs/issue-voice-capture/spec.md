## ADDED Requirements

### Requirement: Issue creation supports voice-driven draft capture
The system SHALL let users start a voice capture flow from the create-issue experience, generate transcript text when transcription capability is available, and merge that text into the issue draft before the issue is created.

#### Scenario: Using voice capture when the title is empty
- **WHEN** a user starts voice capture from the create-issue flow while the title field is empty
- **AND** the capture produces transcript text
- **THEN** the system proposes a title candidate from the first sentence or first line of the transcript
- **AND** writes the full transcript into the issue description draft
- **AND** lets the user edit both fields before creating the issue

#### Scenario: Using voice capture when the title already exists
- **WHEN** a user starts voice capture from the create-issue flow while the title field already contains text
- **AND** the capture produces transcript text
- **THEN** the system keeps the existing title unchanged
- **AND** appends the full transcript into the issue description draft

#### Scenario: Transcript is too short to form a title candidate
- **WHEN** a voice capture result contains transcript text that is too short to form a usable title candidate
- **THEN** the system keeps the title unset or unchanged
- **AND** still writes the transcript into the description draft
- **AND** tells the user that the title still needs manual confirmation

### Requirement: Issue creation can preserve the original voice note as an issue attachment
The system SHALL allow users to keep the original captured audio and, when they choose to keep it, SHALL upload that audio as an attachment linked to the newly created issue after the issue create request succeeds.

#### Scenario: Keeping the original recording
- **WHEN** a user completes voice capture, chooses to keep the original recording, and successfully creates the issue
- **THEN** the system uploads the recorded audio after issue creation succeeds
- **AND** links the uploaded audio to the created issue as an attachment

#### Scenario: Not keeping the original recording
- **WHEN** a user completes voice capture but chooses not to keep the original recording
- **THEN** the system creates the issue from the transcript-enhanced draft without uploading an audio attachment

#### Scenario: Attachment upload fails after issue creation
- **WHEN** the issue is created successfully but the follow-up audio attachment upload fails
- **THEN** the issue remains created successfully
- **AND** the user is told that the transcript was saved but the audio recording was not preserved

### Requirement: Voice capture degrades according to detected client capabilities
The system SHALL explicitly detect whether the client can record audio, transcribe audio, or neither, and SHALL adapt the create-issue voice experience accordingly without breaking manual issue creation.

#### Scenario: Browser supports recording and transcription
- **WHEN** the client supports both audio recording and browser-native transcription
- **THEN** the voice capture UI allows the user to record and review transcript text in the create-issue flow

#### Scenario: Browser supports recording but not browser-native transcription
- **WHEN** the client supports audio recording but does not support browser-native transcription
- **THEN** the system makes that limitation explicit to the user
- **AND** keeps the manual issue creation flow usable
- **AND** may allow recording-only behavior if that mode is enabled by product configuration

#### Scenario: Browser does not support voice capture
- **WHEN** the client supports neither the required recording nor the supported fallback path
- **THEN** the voice input control is hidden or disabled with an explanation
- **AND** the rest of the create-issue flow remains available

### Requirement: Server-side transcription is modeled as a separate capability from text chat completion
The system SHALL treat server-side audio transcription as a separate product and configuration capability from existing workspace text-completion AI settings.

#### Scenario: Workspace has text AI configured but no transcription capability
- **WHEN** a workspace has text AI settings configured for label or schedule suggestions
- **THEN** the system does not assume that audio transcription is also available

#### Scenario: Server-side transcription is enabled later
- **WHEN** a future implementation enables server-side transcription for voice capture fallback
- **THEN** the system uses explicit transcription capability configuration rather than implicitly reusing text chat-completion settings