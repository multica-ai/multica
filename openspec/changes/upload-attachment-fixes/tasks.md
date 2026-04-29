## 1. Backend upload metadata behavior

- [ ] 1.1 Add extension-aware content-type overrides in the upload handler for known sniffing mismatches such as SVG.
- [ ] 1.2 Update storage uploads to use inline disposition only for previewable media and attachment disposition for other file types while preserving sanitized filenames.
- [ ] 1.3 Add backend coverage for upload content-type and content-disposition behavior.

## 2. Workspace attachment-link semantics

- [ ] 2.1 Update `apps/workspace` issue description uploads so embedded files no longer send `issue_id` during upload.
- [ ] 2.2 Keep comment, reply, and comment-edit upload flows linked to issue and comment context where applicable.
- [ ] 2.3 Add or update workspace coverage proving description embeds no longer appear in issue attachment surfaces while comment uploads still do.

## 3. Issue list payload optimization

- [ ] 3.1 Change `ListIssues` to return summary fields only instead of full issue rows.
- [ ] 3.2 Regenerate sqlc artifacts and update backend list-response conversion so list responses keep required metadata while returning `description` as empty detail content.
- [ ] 3.3 Add backend tests proving list responses stay compatible with backlog, today, and upcoming semantics while single-issue reads still return full descriptions.

## 4. Verification

- [ ] 4.1 Verify representative image, PDF, and generic-file uploads from `apps/workspace` for expected preview versus download behavior.
- [ ] 4.2 Run the relevant backend and workspace tests before archiving the change.