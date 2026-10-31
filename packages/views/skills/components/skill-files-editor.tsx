"use client";

import { useState } from "react";
import { Plus, Trash2 } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { cn } from "@multica/ui/lib/utils";
import { PAGE_GUTTER, PAGE_RAIL } from "../../layout/page-header";
import { useT } from "../../i18n";
import { FileTree } from "./file-tree";
import { FileViewer, isMarkdownPath, type FileMode } from "./file-viewer";

export const SKILL_MD = "SKILL.md";

// File path validation + inline add

export function useValidateNewFilePath() {
  const { t } = useT("skills");
  return (path: string, existing: string[]): string => {
    const p = path.trim();
    if (!p) return t(($) => $.detail.add_file.errors.empty);
    if (p.startsWith("/")) return t(($) => $.detail.add_file.errors.absolute);
    if (p.split("/").includes("..")) return t(($) => $.detail.add_file.errors.double_dot);
    if (p === SKILL_MD) return t(($) => $.detail.add_file.errors.reserved);
    if (existing.includes(p)) return t(($) => $.detail.add_file.errors.exists);
    // Directories are inferred from paths, not stored, so a file named after
    // one merges into that folder's node when the tree is built — it drops off
    // the rail while still sitting in the draft. Rejected in both directions:
    // a file cannot take a folder's name, and cannot take a name that an
    // existing file is already nested under.
    if (existing.some((other) => other.startsWith(`${p}/`))) {
      return t(($) => $.detail.add_file.errors.is_directory);
    }
    if (existing.some((other) => p.startsWith(`${other}/`))) {
      return t(($) => $.detail.add_file.errors.under_file);
    }
    return "";
  };
}

function AddFileInline({
  existingPaths,
  onAdd,
  onCancel,
}: {
  existingPaths: string[];
  onAdd: (path: string) => void;
  onCancel: () => void;
}) {
  const { t } = useT("skills");
  const validate = useValidateNewFilePath();
  const [path, setPath] = useState("");
  const [error, setError] = useState("");

  const submit = () => {
    const err = validate(path, existingPaths);
    if (err) {
      setError(err);
      return;
    }
    onAdd(path.trim());
  };

  return (
    <div className="mt-1.5">
      <Input
        autoFocus
        value={path}
        onChange={(e) => {
          setPath(e.target.value);
          setError("");
        }}
        onKeyDown={(e) => {
          if (e.key === "Enter") submit();
          if (e.key === "Escape") onCancel();
        }}
        placeholder={t(($) => $.detail.add_file.placeholder)}
        className="h-7 font-mono text-caption"
      />
      {error && (
        <p role="alert" className="mt-1 text-caption text-destructive">
          {error}
        </p>
      )}
      <div className="mt-1.5 flex items-center gap-1.5">
        <Button type="button" size="xs" onClick={submit}>
          {t(($) => $.detail.add_file.add)}
        </Button>
        <Button type="button" size="xs" variant="ghost" onClick={onCancel}>
          {t(($) => $.detail.add_file.cancel)}
        </Button>
      </div>
    </div>
  );
}

/**
 * The skill's files as a tree beside the open file. Wider than an object
 * page's reading column, so it fills the page body on its own rather than
 * sitting in a section.
 */
export function SkillFilesEditor({
  filePaths,
  selectedPath,
  selectedContent,
  mode,
  canEdit,
  addingFile,
  focusEditor,
  onSelectPath,
  onModeChange,
  onStartAddFile,
  onAddFile,
  onCancelAddFile,
  onDeleteFile,
  onRenameFile,
  onEditFile,
  onContentChange,
  onFocusHandled,
}: {
  filePaths: string[];
  selectedPath: string;
  selectedContent: string;
  mode: FileMode;
  canEdit: boolean;
  addingFile: boolean;
  focusEditor: boolean;
  onSelectPath: (path: string) => void;
  onModeChange: (mode: FileMode) => void;
  onStartAddFile: () => void;
  onAddFile: (path: string) => void;
  onCancelAddFile: () => void;
  onDeleteFile: (path?: string) => void;
  onRenameFile: (from: string, to: string) => void;
  onEditFile: (path: string) => void;
  onContentChange: (content: string) => void;
  onFocusHandled: () => void;
}) {
  const { t } = useT("skills");
  const validatePath = useValidateNewFilePath();
  const supportingPaths = filePaths.filter((p) => p !== SKILL_MD);
  const isMd = isMarkdownPath(selectedPath);
  // Absent for read-only viewers so the tree never offers an action it would
  // then refuse. Both rails get the same object: SKILL.md keeps Edit and loses
  // rename/delete, which the tree derives from reservedPath.
  const treeActions = canEdit
    ? {
        onEdit: onEditFile,
        validatePath,
        onRename: onRenameFile,
        onDelete: onDeleteFile,
        reservedPath: SKILL_MD,
      }
    : undefined;

  return (
    <div className={cn(PAGE_RAIL, "flex min-h-full flex-col md:h-full md:flex-row")}>
      <aside
        role="tablist"
        aria-orientation="vertical"
        aria-label={t(($) => $.detail.files.list_aria)}
        className={cn(
          "shrink-0 border-b border-surface-border py-3 md:w-52 md:overflow-y-auto md:border-b-0 md:border-r md:py-4",
          PAGE_GUTTER,
        )}
      >
        <p className="px-2.5 pb-1 text-micro font-semibold uppercase tracking-wider text-muted-foreground">
          {t(($) => $.detail.files.main)}
        </p>
        <FileTree
          actions={treeActions}
          filePaths={[SKILL_MD]}
          selectedPath={selectedPath}
          onSelect={onSelectPath}
        />

        <p className="px-2.5 pb-1 pt-3 text-micro font-semibold uppercase tracking-wider text-muted-foreground">
          {t(($) => $.detail.files.supporting, { count: supportingPaths.length })}
        </p>
        {supportingPaths.length > 0 ? (
          <FileTree
            actions={treeActions}
            filePaths={supportingPaths}
            selectedPath={selectedPath}
            onSelect={onSelectPath}
          />
        ) : (
          !addingFile && (
            <p className="px-2.5 py-1 text-caption text-muted-foreground">
              {t(($) => $.detail.files.supporting_empty)}
            </p>
          )
        )}

        {canEdit &&
          (addingFile ? (
            <AddFileInline
              existingPaths={filePaths}
              onAdd={onAddFile}
              onCancel={onCancelAddFile}
            />
          ) : (
            <Button
              type="button"
              variant="ghost"
              size="xs"
              onClick={onStartAddFile}
              className="mt-2 h-8 w-full justify-start px-2.5 text-muted-foreground"
            >
              <Plus className="h-3.5 w-3.5" />
              {t(($) => $.detail.files.add_file)}
            </Button>
          ))}
      </aside>

      <section className="flex min-h-[32rem] min-w-0 flex-1 flex-col md:min-h-0">
        <div className="flex h-10 shrink-0 items-center gap-3 border-b px-3 sm:px-4">
          <span className="truncate font-mono text-caption text-muted-foreground">
            {selectedPath}
          </span>
          <div className="ml-auto flex shrink-0 items-center gap-1">
            {isMd && (
              // The second segment is named for what it does for THIS viewer:
              // "Edit" when the pane it opens accepts typing, "Plain text"
              // when the same pane is read-only. Same mode either way — only
              // the promise differs, and offering an edit the page would
              // refuse is the thing this rail is careful not to do.
              <div
                role="group"
                aria-label={t(($) => $.detail.files.mode_aria)}
                className="flex items-center gap-0.5 rounded-md bg-muted p-0.5"
              >
                {(["preview", "raw"] as const).map((value) => (
                  <button
                    key={value}
                    type="button"
                    aria-pressed={mode === value}
                    onClick={() => onModeChange(value)}
                    className={cn(
                      "h-6 rounded-xs px-2 text-caption font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                      mode === value
                        ? "bg-surface text-foreground shadow-sm"
                        : "text-muted-foreground hover:text-foreground",
                    )}
                  >
                    {value === "preview"
                      ? t(($) => $.detail.files.mode_preview)
                      : canEdit
                        ? t(($) => $.detail.files.mode_edit)
                        : t(($) => $.detail.files.mode_raw)}
                  </button>
                ))}
              </div>
            )}
            {selectedPath !== SKILL_MD && canEdit && (
              <Tooltip>
                <TooltipTrigger
                  render={
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon-sm"
                      onClick={() => onDeleteFile()}
                      className="text-muted-foreground hover:text-destructive"
                      aria-label={t(($) => $.detail.delete_file)}
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                    </Button>
                  }
                />
                <TooltipContent>{t(($) => $.detail.delete_file)}</TooltipContent>
              </Tooltip>
            )}
          </div>
        </div>

        <div className="min-h-0 flex-1">
          <FileViewer
            key={selectedPath}
            path={selectedPath}
            content={selectedContent}
            mode={mode}
            readOnly={!canEdit}
            autoFocus={focusEditor}
            onChange={onContentChange}
            onFocusHandled={onFocusHandled}
          />
        </div>
      </section>
    </div>
  );
}
