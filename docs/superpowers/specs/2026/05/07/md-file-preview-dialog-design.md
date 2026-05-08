# Markdown File Preview Dialog Design

## Context

Markdown attachments are rendered as file cards in editor and readonly content
surfaces. For `.md` and `.markdown` files, the card shows a preview button that
fetches the markdown through `api.previewAttachmentMarkdown()` and renders it in
a dialog using the existing `ReadonlyContent` pipeline.

The current preview dialog has a fixed centered size. Long or wide markdown
files can be hard to inspect because users cannot resize the preview, move it
away from surrounding content, or expand it to a full-screen view.

## Product Behavior

The markdown preview dialog should behave like an inspectable document window.
It opens centered at a default size, can be moved by dragging its header, can be
resized from its edges and corners, and includes a full-screen toggle in the
top-right action area.

User-adjusted size, position, and full-screen state are temporary. Closing the
dialog resets all layout state so the next preview opens centered with the
default dimensions.

## Interaction Details

- Opening a markdown preview keeps the existing fetch behavior:
  - Open the dialog immediately.
  - Show the existing loading text while content is fetched.
  - Render fetched content through the provided `renderContent` callback.
  - On fetch failure, log the error, show the existing toast, and close the
    dialog.
- In normal mode:
  - Drag the header area to move the dialog.
  - Drag edges or corners to resize the dialog.
  - Keep the markdown body in its own scrollable region.
  - Keep the dialog constrained to the browser viewport enough that controls
    remain reachable.
- In full-screen mode:
  - The dialog fills the available viewport.
  - Dragging and resizing are disabled while full-screen is active.
  - The markdown body remains scrollable.
  - Clicking the full-screen button again restores the size and position from
    before full-screen was entered.
- The full-screen button sits to the left of the close button and uses
  `Maximize2` / `Minimize2` icons with accessible labels.
- The close button remains available in normal and full-screen modes.

## Architecture

Use a dedicated draggable/resizable wrapper for markdown previews instead of
changing the shared `DialogContent` behavior. The shared dialog primitive still
provides modal behavior, backdrop, focus handling, and close behavior, while a
new markdown-preview-specific content component owns geometry and full-screen
state.

The implementation will use a focused drag/resize dependency such as
`react-rnd`, added to `@multica/views`. This matches the confirmed approach and
avoids rebuilding low-level pointer handling for a modal window.

## Implementation Scope

- Add the drag/resize dependency to `packages/views`.
- Update `packages/views/editor/markdown-file-preview.tsx` so markdown preview
  dialogs render through a dedicated draggable/resizable preview content
  component.
- Keep markdown preview behavior scoped to `.md` and `.markdown` file cards.
- Add full-screen and exit-full-screen controls.
- Add stable test hooks only where needed for behavior tests.
- Preserve existing preview trigger, download button ordering, relative URL
  support, loading state, and failure behavior.

## Testing

Add focused tests to the existing editor markdown/file-card test coverage:

- Markdown preview still fetches and renders content.
- The preview dialog exposes a draggable header, a resizable shell, and a
  scrollable markdown body.
- The full-screen button switches to full-screen mode and changes to an exit
  full-screen control.
- Exiting full-screen restores normal dialog mode.
- Relative markdown previews still call `previewAttachmentMarkdown()` with the
  original relative path.
- Existing download button order and loading/failure behavior do not regress.

## Out of Scope

- Persisting dialog size, position, or full-screen state across openings.
- Adding content zoom controls.
- Changing non-markdown file cards.
- Changing image lightbox behavior.
- Making all application dialogs draggable or resizable.
