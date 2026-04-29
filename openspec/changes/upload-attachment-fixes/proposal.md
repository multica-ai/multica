## Why

The current workspace upload flow has several inconsistencies that are now more visible because the product is leaning harder on rich issue descriptions, comments, and list-driven planning views:

- The server currently relies on `http.DetectContentType` alone, which misclassifies files such as SVG and can break inline rendering after upload.
- Uploaded objects are always stored with `Content-Disposition: inline`, so non-media files open in the browser when users often expect a download.
- In `apps/workspace`, issue description uploads are linked to the issue attachment surface even though the file URL already lives inside the markdown body, which leaves stale attachment lists when an embed is removed.
- `ListIssues` still returns the full description payload, so embedded upload URLs and rich text inflate the list responses used by board, backlog, today, and upcoming views.

This work does not conflict with the active `gradual-project-management-transition` or `issue-start-end-dates` changes. Both continue to rely on `issue` as the canonical model, and both benefit from more reliable upload metadata plus leaner issue list payloads.

## What Changes

- Make upload content-type selection extension-aware for known sniffing failures while keeping server-side inspection as the default source of truth.
- Store uploaded objects with preview-friendly or download-friendly `Content-Disposition` metadata based on effective file type.
- Update `apps/workspace` so description-editor uploads stop linking embedded files into issue attachment surfaces, while comment and reply uploads remain linked for issue/comment discovery.
- Narrow list-oriented issue queries so they do not include full description bodies when only summary fields are needed.
- Add or update backend and workspace coverage for the new upload, attachment, and list-response semantics.

## Capabilities

### New Capabilities

- `upload-attachment-consistency`: Keep file upload metadata, attachment-link behavior, and issue list payloads consistent with the current workspace-first product architecture.

## Impact

- `server/internal/handler/file.go` upload parsing and attachment creation behavior.
- `server/internal/storage/s3.go` object metadata written to storage.
- `server/pkg/db/queries/issue.sql` and backend issue list response conversion.
- `apps/workspace` issue description, comment, and reply upload flows.
- Attachment list and attachment-discovery behavior used by workspace UI, CLI download flows, and agent-facing issue context.