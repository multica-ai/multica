# Issue Voice Capture — Tasks

## 1. OpenSpec And Design

- [x] 1.1 Align proposal with Cloudflare non-streaming Phase 1 and provider-based future expansion.
- [x] 1.2 Add research evidence for current code paths and provider pricing/capability decisions.
- [x] 1.3 Update design with ASCII data flow and state machine diagrams.
- [x] 1.4 Update requirement spec for non-streaming transcription, provider isolation, and future streaming compatibility.

## 2. Backend Transcription Foundation

- [x] 2.1 Add `server/internal/service/transcription.go`.
  - Goal: Define `TranscriptionProvider`, `TranscriptionInput`, `TranscriptionResult`, and provider config resolution.
  - Done when: The service can select a configured provider and return provider-disabled errors when configuration is missing.
  - Verification: Go unit tests for config resolution and disabled-provider behavior.

- [x] 2.2 Add Cloudflare provider implementation.
  - Goal: Call Cloudflare Workers AI Whisper through the provider interface.
  - Files: `server/internal/service/transcription_cloudflare.go`.
  - Done when: Provider sends audio to Cloudflare, parses transcript text, and returns normalized `TranscriptionResult`.
  - Verification: Go tests with mocked HTTP client/server.

- [x] 2.3 Add `server/internal/handler/transcription.go`.
  - Goal: Implement `POST /api/transcriptions` as authenticated multipart upload.
  - Done when: Handler validates file presence, content type, max size, workspace membership, provider config, and provider errors.
  - Verification: Handler tests for success, missing file, unsupported type, provider disabled, and provider failure.

- [x] 2.4 Register transcription route.
  - Goal: Add protected route in `cmd/server/router.go`.
  - Done when: Authenticated workspace users can call `POST /api/transcriptions`.
  - Verification: Route-level handler test or existing router test coverage.

## 3. Frontend Voice Capture Foundation

- [x] 3.1 Add `apps/workspace/src/features/issues/hooks/use-issue-voice-recorder.ts`.
  - Goal: Encapsulate `MediaRecorder`, microphone permission, mime selection, duration, blob, and recorder state.
  - Done when: Hook exposes provider-agnostic recording state and audio output.
  - Verification: Vitest hook tests with mocked browser APIs.

- [x] 3.2 Add `apps/workspace/src/features/issues/hooks/use-issue-transcription.ts`.
  - Goal: Encapsulate non-streaming API call to `POST /api/transcriptions`.
  - Done when: Hook accepts recorded audio and returns transcript result or normalized error.
  - Verification: Vitest tests with mocked API client.

- [x] 3.3 Add transcript mapping utility.
  - Goal: Keep title/description mapping testable outside the modal.
  - Suggested file: `apps/workspace/src/features/issues/utils/voice-transcript.ts`.
  - Done when: Utility implements empty-title, existing-title, existing-description, and too-short-title rules.
  - Verification: Unit tests for all mapping rules.

## 4. Create Issue Modal Integration

- [x] 4.1 Add microphone control to `CreateIssueModal`.
  - Goal: Let users start/stop recording from the existing create issue footer or toolbar.
  - Done when: The control reflects idle, recording, transcribing, review, error, and unsupported states.
  - Verification: Component tests for visible states.

- [x] 4.2 Add review UI.
  - Goal: Show transcript preview, insert/discard actions, and keep-recording option.
  - Done when: Insert applies mapping rules without submitting the issue.
  - Verification: Component tests for insert and discard behavior.

- [x] 4.3 Upload optional raw recording after issue creation.
  - Goal: Preserve audio only when the user opts in and issue creation succeeds.
  - Done when: `handleSubmit` creates issue first, then calls existing upload path with `issueId`.
  - Verification: Component test asserts create-before-upload order and attachment failure toast behavior.

## 5. Future Streaming Compatibility

- [x] 5.1 Keep UI provider-agnostic.
  - Goal: Avoid Cloudflare or Doubao-specific state in `CreateIssueModal`.
  - Done when: Provider names are not referenced in create issue UI logic except optional generic error copy from API.
  - Verification: Code review and targeted search.

- [x] 5.2 Keep non-streaming and streaming contracts separate.
  - Goal: Do not design chunking or partial transcript into `POST /api/transcriptions`.
  - Done when: Future streaming is documented as separate session/WebSocket API only.
  - Verification: Design review.

## 6. Verification

- [x] 6.1 Run targeted frontend tests for voice hooks, mapping utility, and create issue modal.
- [x] 6.2 Run targeted backend tests for transcription service and handler.
- [x] 6.3 Run typecheck for workspace frontend after integration.
- [x] 6.4 Run Go tests for touched backend packages.
- [x] 6.5 Run broader verification only if explicitly requested.
- [x] 6.6 Complete real Cloudflare smoke test after a valid API token is available.

## 7. Acceptance Criteria Gate

- [x] AC1 Logged-in workspace member can record audio from create issue and see non-streaming transcription progress.
- [x] AC2 Transcript review appears before title or description mutation.
- [x] AC3 Empty-title transcript insertion fills a title candidate and appends full transcript to description.
- [x] AC4 Existing-title transcript insertion preserves title and appends transcript to description.
- [x] AC5 Existing-description transcript insertion appends after a blank line.
- [x] AC6 Discarding transcript review leaves the issue draft unchanged.
- [x] AC7 Recording unsupported, permission denied, provider disabled, and provider failure keep manual issue creation usable.
- [x] AC8 `POST /api/transcriptions` is authenticated, workspace-scoped, multipart, and non-streaming.
- [x] AC9 Backend transcription uses `TranscriptionProvider` and does not reuse text AI settings.
- [x] AC10 Cloudflare provider returns normalized transcript response when configured.
- [x] AC11 Missing provider config returns provider-not-configured error.
- [x] AC12 Raw audio is uploaded as attachment only after issue creation succeeds and user opts in.
- [x] AC13 Audio attachment upload failure does not roll back issue creation and shows a distinct warning.
- [x] AC14 Phase 1 contains no WebSocket streaming or partial transcript chunk semantics.
- [x] AC15 Create issue UI remains provider-agnostic.
