# Right Sidebar Directory Tree

## Overview

Add a directory tree to the right sidebar that displays the current session's mounted directory (`workingDirectory`). Users can expand/collapse folders and right-click to open files/folders in various applications.

## Scope

- Single "All files" tab (no Changes or Checks tabs)
- File-type-specific icons
- Right-click context menu with auto-detected installed apps
- No toolbar icons (clean header)

## Architecture

### Frontend Components

| Component         | Purpose                                             |
| ----------------- | --------------------------------------------------- |
| `FileTree`        | Main component in RightPanel, manages tree state    |
| `FileTreeItem`    | Individual row: icon, name, expand/collapse chevron |
| `FileContextMenu` | Right-click menu with app options                   |

### Backend (Main Process)

| IPC Handler         | Purpose                                   |
| ------------------- | ----------------------------------------- |
| `fs:list-directory` | Returns immediate children of a directory |
| `fs:detect-apps`    | Detects installed apps on system          |
| `fs:open-with`      | Opens file/folder with specified app      |

### State

- Expanded folders tracked in `uiStore` (Zustand)
- Detected apps cached on app startup

## UI Details

### RightPanel Header

- Title: "All files" (left-aligned)
- No toolbar icons

### Tree Structure

- Root = session's `workingDirectory`
- Folders: chevron on left, expand/collapse on click
- Indentation: 16px per nesting level
- Sorting: folders first, then files, alphabetical
- Hidden files (dotfiles) shown

### File Icons

| Type                      | Icon                    |
| ------------------------- | ----------------------- |
| Folder (closed)           | `FolderIcon`            |
| Folder (open)             | `FolderOpenIcon`        |
| TypeScript (.ts, .tsx)    | Custom TS icon (blue)   |
| JavaScript (.js, .jsx)    | Custom JS icon (yellow) |
| JSON (.json)              | `{ }` icon              |
| YAML (.yaml, .yml)        | Custom YAML icon        |
| Markdown (.md)            | Custom MD icon          |
| Images (.png, .jpg, .svg) | `ImageIcon`             |
| Default                   | Generic `FileIcon`      |

### Interactions

- Single click folder: expand/collapse
- Right click any item: show context menu

## Context Menu

### App Detection (macOS)

Check for existence in `/Applications/` and `~/Applications/`:

| App      | Detection                |
| -------- | ------------------------ |
| Finder   | Always available         |
| Cursor   | `Cursor.app`             |
| VS Code  | `Visual Studio Code.app` |
| Xcode    | `Xcode.app`              |
| Ghostty  | `Ghostty.app`            |
| iTerm    | `iTerm.app`              |
| Terminal | Always available         |

### Menu Structure

```
Finder          ⌘O    ← Always first
─────────────────────
Cursor                ← Editors (if installed)
VS Code
Xcode
─────────────────────
Ghostty               ← Terminals (if installed)
iTerm
Terminal
─────────────────────
Copy path             ← Always last
```

### Opening Behavior

| App       | Command                         |
| --------- | ------------------------------- |
| Finder    | `open -R <path>` (reveal)       |
| Editors   | `open -a "App Name" <path>`     |
| Terminals | `open -a "Terminal" <path>`     |
| Copy path | Copy absolute path to clipboard |

## Data Flow

### Directory Listing

1. RightPanel opens with session selected
2. Call `fs:list-directory(workingDirectory)`
3. Returns shallow list (immediate children only)
4. When folder expanded → lazy load children

### Data Structure

```typescript
interface FileTreeNode {
  name: string
  path: string
  type: 'file' | 'directory'
  extension?: string
}
```

### Performance

- Lazy loading: only fetch on expand
- No file watching (manual refresh via re-expand)
- Cache expanded contents in memory

### Edge Cases

| Case              | Handling                    |
| ----------------- | --------------------------- |
| Permission denied | Grayed out with lock icon   |
| Empty directory   | Show "(empty)" text         |
| Symlinks          | Follow, show as target type |

## Implementation Steps

1. Add IPC handlers in main process (`fs:list-directory`, `fs:detect-apps`, `fs:open-with`)
2. Create `FileTreeItem` component with icons and expand/collapse
3. Create `FileTree` component with lazy loading
4. Create `FileContextMenu` component
5. Integrate into `RightPanel`
6. Add expanded state to `uiStore`
