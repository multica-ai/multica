Purpose: Verify that issue attachments support direct OBS/S3 upload, invalid-id validation, UUIDv7 IDs, preview/download, delete, and append-to-description actions.

Preconditions: The Multica web app is reachable. The user is signed in. The backend is configured either with local storage or OBS/S3 storage. A disposable issue exists.

User flow:
1. Open the disposable issue and attach a small image plus a small text/PDF file.
2. Verify upload progress and successful attachment rendering in the issue attachment area.
3. Preview the image and download/open the file attachment.
4. Use the attachment action menu to append the attachment to the issue description.
5. Delete one attachment and verify it disappears from the attachment area and is not referenced in the description unless intentionally appended before deletion.
6. Create an issue through the API or UI with an invalid attachment ID and verify the request is rejected with a clear error.
7. Repeat attachment creation with a UUIDv7-style attachment ID if the fixture can call the upload API directly.

Expected results:
- Direct upload uses the configured storage path and creates usable attachment records.
- Preview/download URLs are generated lazily and work for local, legacy local, and OBS/S3-backed attachments.
- Historical CDN URL remapping keeps old attachment links readable.
- Invalid attachment IDs are rejected during issue creation instead of creating broken references.
- UUIDv7 attachment IDs are accepted where valid attachment records exist.
- Append-to-description and delete actions update the UI without requiring a full page reload.

Notes for automation: Prefer a small deterministic fixture file. For OBS/S3 checks, record whether the environment used real object storage or local storage fallback.
