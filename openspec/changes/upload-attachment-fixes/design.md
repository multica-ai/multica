## Context

Multica's product surface now lives primarily in `apps/workspace`, while `apps/web` serves the marketing site. Rich issue descriptions and threaded comments already support file uploads through the shared workspace upload hook and the server-side `/api/upload-file` endpoint. Uploaded files are persisted through the `attachment` table and stored in S3-compatible object storage.

The current implementation has three practical mismatches with product expectations:

- Upload metadata is derived from `http.DetectContentType` alone, which is correct for many files but wrong for some extension-sensitive types such as SVG.
- Storage always writes `Content-Disposition: inline`, which makes browsers preview files that should behave like downloads.
- Description-editor uploads are treated like issue attachments even though they are embedded content inside the markdown body, so the issue attachment list can drift away from what the description actually contains.

There is also a list-performance problem: `ListIssues` still selects full issue rows, including `description`, even though planning surfaces only need summary metadata. The new project-management views in `apps/workspace` make this over-fetching more visible because rich descriptions can now include uploaded media URLs.

Constraints that shape the design:

- `apps/workspace` is the product app for this change; `apps/web` should not become the target for issue-upload UX work.
- The backend already uses the `attachment` model for comment-linked files, CLI download flows, and agent attachment discovery, so comment and reply uploads must keep their linkage behavior.
- `issue` remains the canonical work item, and the active project-management change already depends on additive, backward-compatible list semantics.
- This change should harden current behavior, not redesign the editor UI or replace the attachment model entirely.

## Goals / Non-Goals

**Goals:**

- Make uploaded files render or download according to effective file type rather than accidental browser defaults.
- Prevent embedded description uploads from surfacing as issue attachments.
- Preserve linked attachment behavior for comments and replies.
- Reduce issue list payload size by excluding full description bodies from list-oriented reads.
- Keep the change aligned with the current workspace-first architecture.

**Non-Goals:**

- A new upload UI, file-card renderer, lightbox, or drag-and-drop redesign.
- Removing the `attachment` table or generic upload endpoint.
- Changing how comment and reply attachments are linked or discovered.
- Reworking `apps/web` marketing pages.
- Full attachment garbage collection for unlinked workspace uploads.

## Decisions

### 1. Keep sniff-first content detection, with extension-aware overrides for known mismatches

The upload handler will continue to inspect file bytes on the server, but it will apply a small extension-based override map for types that Go commonly misdetects, starting with SVG.

Why this approach:

- It keeps the server, not the client, as the source of truth for upload metadata.
- It fixes real rendering failures without falling back to a brittle extension-only strategy.
- It limits the blast radius to known misdetections rather than introducing a large MIME allowlist.

Alternatives considered:

- Trust the browser-provided content type: rejected because client headers are easier to spoof and often inconsistent.
- Use extension-only detection for every upload: rejected because byte sniffing remains more reliable for most files.

### 2. Choose `Content-Disposition` from previewability, not a single global default

Storage writes will keep inline behavior for previewable media such as images, video, audio, and PDFs, while other files default to `attachment` so browsers download them.

Why this approach:

- It matches user expectations better than forcing every file into the browser preview path.
- It preserves inline rendering for the files that are intentionally embedded in issue descriptions.
- It keeps the behavior centralized in server-side storage metadata instead of relying on each client surface to special-case download behavior.

Alternatives considered:

- Keep `inline` for everything: rejected because it produces poor behavior for source files, archives, and office-style documents.
- Force `attachment` for everything: rejected because it would break current image and media preview flows.

### 3. Treat description-editor uploads as embedded content, not issue attachments

`apps/workspace` issue description uploads will stop sending `issue_id` during upload. The uploaded URL will still be embedded into markdown, but the file will no longer appear in issue attachment lists that are meant to represent explicit issue/comment attachments. Comment and reply uploads will continue to send issue and comment context so agent and CLI attachment discovery keeps working.

Why this approach:

- The markdown body is already the source of truth for description embeds.
- It removes the stale-record problem from the issue attachment surface without changing comment attachment workflows.
- It preserves existing discovery semantics for conversation files, which are still meaningful as explicit attachments.

Alternatives considered:

- Keep linking description uploads to the issue and attempt bidirectional sync when embeds are removed: rejected because it adds fragile editor diffing to a problem that can be avoided entirely.
- Stop creating any attachment record whenever `issue_id` is absent: rejected for now because the server may still need generic workspace upload bookkeeping, and that broader cleanup problem is separate.

### 4. Return summary rows for list views and keep full content for detail reads

Issue list queries will select only the fields needed for list, board, backlog, today, and upcoming surfaces. Single-issue reads will continue to return the full description. Backend list conversion will explicitly produce `description: null` for list responses rather than returning full markdown bodies.

Why this approach:

- It directly addresses the payload growth caused by embedded file URLs in descriptions.
- It fits the current project-management direction, which depends on list-heavy derived views.
- It avoids changing detail reads or editor behavior.

Alternatives considered:

- Keep `SELECT *` and accept larger list payloads: rejected because the list surfaces do not need full description bodies.
- Add a separate list-summary endpoint: rejected because the current backend direction favors additive compatibility on existing issue list behavior.

### 5. Scope the frontend work to `apps/workspace`

The upload/editor behavior change will be defined for `apps/workspace`, because that is where the current issue editing and threaded discussion product lives. `apps/web` remains out of scope unless a later change adds shared upload behavior back into the marketing site or another product surface.

Why this approach:

- It matches the current repository architecture.
- It avoids reviving outdated assumptions from older plans that treated `apps/web` as the main product client.
- It keeps the change small and directly implementable.

## Risks / Trade-offs

- [Some unlinked workspace upload records may still exist after description uploads] -> Mitigation: keep issue attachment surfaces driven by issue/comment linkage now, and treat broader cleanup or retention policy as a follow-up.
- [Content-disposition rules may need refinement for specific file types] -> Mitigation: start with a small, explicit previewable-media rule and extend only when a concrete need appears.
- [List-response projection changes could break callers that accidentally relied on list descriptions] -> Mitigation: keep single-issue detail behavior unchanged and add coverage for list/detail compatibility.
- [Comment attachment discovery could regress if description and comment upload paths are conflated] -> Mitigation: preserve issue/comment linkage in comment and reply upload flows and test those flows explicitly.

## Migration Plan

1. Harden upload content-type and content-disposition behavior in the backend.
2. Update workspace description upload flows so embedded files stop linking into issue attachment surfaces.
3. Keep comment and reply upload linkage behavior intact.
4. Narrow issue list query projections and adjust backend list-response conversion.
5. Verify upload behavior across representative file types and confirm list/detail response compatibility.

## Open Questions

None for this change.