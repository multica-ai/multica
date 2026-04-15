## What does this PR do?

This PR significantly enhances the core capabilities of the issue editor to improve collaboration and documentation efficiency. It transforms the basic editor into a more robust tool by introducing a production-ready **Table Editing System**, seamless **Excel/Spreadsheet integration**, and **Inline Attachment Previews**. 

Beyond new features, it refactors the comment input logic into a modular component to ensure a consistent UX across the platform. This approach respects the solid foundation laid by previous contributors while addressing real-world productivity needs, such as moving data from spreadsheets and instantly accessing uploaded information.

## Related Issue

Closes #

## Type of Change

- [ ] Bug fix (non-breaking change that fixes an issue)
- [x] New feature (non-breaking change that adds functionality)
- [x] Refactor / code improvement (no behavior change)
- [ ] Documentation update
- [ ] Tests (adding or improving test coverage)
- [ ] CI / infrastructure

## Changes Made

### Advanced Table System
- **Excel/Spreadsheet Integration**: Implemented logic in `packages/views/editor/content-editor.tsx` to preserve table structures and basic styles when pasting from Excel or Google Sheets.
- **Table Management Tools**: Added comprehensive toolbar controls for inserting tables, managing rows/columns (CRUD), merging/splitting cells, and text alignment.
- **Visual Refinement**: Updated `packages/views/editor/content-editor.css` to provide professional styling for tables in both edit and read-only modes.

### Enhanced Attachment Handling
- **Inline Previews**: Added support for Markdown, Text, Code, and PDF file previews within `packages/views/editor/extensions/file-card.tsx`, eliminating unnecessary downloads.
- **UI Consistency**: Optimized attachment card dimensions and typography to match the "Agent Activity" status cards, ensuring a cohesive visual hierarchy.

### Refined Upload & Drag-and-Drop UX
- **Batch Processing**: Migrated to a batch-based `uploadFiles` system to prevent UI jitter and empty line accumulation during multi-file drops.
- **Intelligent Insertion**: Improved cursor placement logic to reuse existing trailing empty paragraphs when inserting new block elements.
- **Fluid Drag Overlay**: Enhanced `useFileDropZone` with robust `dragDepth` management and a redesigned rounded overlay that specifically targets the editor body.

### Architectural Improvements
- **Modular Components**: Extracted the standardized comment input into `packages/views/common/comment-input.tsx` for cross-platform reusability.
- **Package Exports**: Updated `packages/views/package.json` to expose the new shared modules.

## How to Test

1. **Excel Integration**: Copy a range of cells from Excel/Google Sheets and paste into the editor. It should render as a native table.
2. **Table Editing**: Verify that the new toolbar icons correctly manage rows, columns, and cell merges.
3. **Batch Upload**: Drop multiple files at once. They should appear grouped together without extra vertical whitespace.
4. **Previews**: Click the "Eye" icon on a PDF or Markdown attachment card to view content inline.
5. **Drag-and-Drop**: Verify the drag overlay correctly avoids the title and toolbar, and disappears immediately upon mouse-leave or drop.

## Checklist

- [x] I have included a thinking path that traces from project context to this change
- [x] I have run tests locally and they pass
- [ ] I have added or updated tests where applicable
- [ ] If this change affects the UI, I have included before/after screenshots
- [x] I have updated relevant documentation to reflect my changes
- [x] I have considered and documented any risks above
- [x] I will address all reviewer comments before requesting merge

## AI Disclosure

**AI tool used:** Multica Agent (Gemini CLI)

**Prompt / approach:**
I built upon the excellent work of previous contributors by analyzing the established Tiptap extension patterns. The approach focused on "polishing the edges"—refactoring sequential file drops into batch operations to fix spacing issues and extending the paste handler to support complex HTML fragments from spreadsheets. The common module extraction ensures that the refined "Issue-style" input becomes the standard for future features.

## Screenshots (optional)

<!-- If applicable, add screenshots showing the change in action. -->
