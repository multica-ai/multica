# @multica/cerebro-artifact-mention

FIR-1800 — render a referenced artifact (document/note) inside a comment, chat
message, DM, or channel body as a **compact white card**.

An agent's output should be saved as an artifact (document/note), not uploaded
as a raw `.md` file. This package renders a reference to such an artifact when
it travels inside a body as a `mention://artifact/<id>` markdown link. The
issue-comment renderer (`@multica/views/editor` `ReadonlyContent`) and the
chat/channel markdown renderer (`@multica/views/common/markdown`) delegate the
`artifact` mention type to `ArtifactMentionChip`.

- **White card = real document.** Grey rows are uploaded files (`--attachment`).
- **Click opens the full-page note editor** (`documentDetail`) — the rule from
  FIR-1782 / PR #1700. Cmd/Ctrl/Shift-click opens it in a new tab.
- **Flag-gated** by `cerebro_artifact_references`. Flag off → the reference
  renders as plain link text, never a card.
- **Folder segment (FIR-2697 part 4).** When `cerebro_attachment_folder` is on
  and the artifact has a folder, the card also shows and links to that folder
  (`documentsFolder` / `notesFolder` by surface). The server guarantees an
  attached agent document always has a folder, so the segment is meaningful; it
  is simply hidden for a legacy folder-less reference. Rendering is covered by
  `artifact-chip.test.tsx`.

The CLI side (`multica issue comment add --artifact <id>`) appends the
`mention://artifact/<id>` token to the comment body.

Kept as its own small package (not folded into `@multica/cerebro-artifacts`) so
the comment/markdown renderers can import it without a turbo build cycle —
`cerebro-artifacts` already depends on `@multica/views/editor`.
