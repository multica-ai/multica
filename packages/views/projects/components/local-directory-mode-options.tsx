"use client";

import { GitBranch, Pencil, TriangleAlert } from "@multica/ui/icons";
import type { LocalDirectoryExecutionMode } from "@multica/core/types";
import { useT } from "../../i18n/use-t";

/**
 * Why the worktree option may be unavailable.
 *
 * Two reasons, and neither is a guess about the machine. `not_git` the client
 * establishes by itself — the folder either has a repository to branch from or
 * it does not, and the desktop picker checked. `server_outdated` is what the
 * SERVER says about itself: whether it understands `execution_mode` at all.
 *
 * Whether the MACHINE can run the mode is deliberately absent. That is the
 * server's question, asked on every save, and a rejection comes back as an
 * inline error rather than as a disabled option guessed at up front (#7113).
 * But deferring to the server is only safe once the server has said it will
 * actually check: older ones drop the field and answer 201, and the task then
 * edits the directory the user asked to isolate.
 *
 * `undefined` means available.
 */
export type WorktreeUnavailableReason = "not_git" | "server_outdated";

interface LocalDirectoryModeOptionsProps {
  value: LocalDirectoryExecutionMode;
  onChange: (mode: LocalDirectoryExecutionMode) => void;
  unavailableReason?: WorktreeUnavailableReason;
}

/**
 * The two-option choice for how runs use a local_directory resource.
 *
 * Shared by the create-project modal, the resources section's add flow and its
 * mode chip, so the same decision offers literally the same options, copy and
 * blocked states wherever it is made.
 *
 * Deliberately does NOT surface the raw `in_place` / `worktree` identifiers as
 * the primary label. The choice a user is actually making is about how they get
 * their results back — edits appearing in their working copy versus a branch
 * they review — so the options lead with that, and the identifier is only a
 * secondary hint for anyone matching this against the CLI or the docs.
 */
export function LocalDirectoryModeOptions({
  value,
  onChange,
  unavailableReason,
}: LocalDirectoryModeOptionsProps) {
  const { t } = useT("projects");
  const worktreeDisabled = unavailableReason !== undefined;

  return (
    <div className="flex flex-col gap-2">
      <ModeOption
        icon={<Pencil className="size-4" />}
        title={t(($) => $.resources.mode_in_place_title)}
        description={t(($) => $.resources.mode_in_place_description)}
        identifier="in_place"
        selected={value === "in_place"}
        onSelect={() => onChange("in_place")}
      />
      <ModeOption
        icon={<GitBranch className="size-4" />}
        title={t(($) => $.resources.mode_worktree_title)}
        description={t(($) => $.resources.mode_worktree_description)}
        identifier="worktree"
        selected={value === "worktree"}
        disabled={worktreeDisabled}
        disabledReason={
          unavailableReason === "not_git"
            ? t(($) => $.resources.mode_worktree_needs_git)
            : unavailableReason === "server_outdated"
              ? t(($) => $.resources.mode_worktree_needs_server_upgrade)
              : undefined
        }
        onSelect={() => onChange("worktree")}
      />
    </div>
  );
}

interface ModeOptionProps {
  icon: React.ReactNode;
  title: string;
  description: string;
  identifier: string;
  selected: boolean;
  disabled?: boolean;
  disabledReason?: string;
  onSelect: () => void;
}

function ModeOption({
  icon,
  title,
  description,
  identifier,
  selected,
  disabled = false,
  disabledReason,
  onSelect,
}: ModeOptionProps) {
  return (
    <button
      type="button"
      role="radio"
      aria-checked={selected}
      disabled={disabled}
      onClick={onSelect}
      // Selection is carried by border + ring rather than a background tint so
      // it stays legible while hovered — hover only moves the background.
      className={`flex w-full items-start gap-3 rounded-lg border p-3 text-left transition-colors ${
        selected
          ? "border-primary ring-1 ring-primary"
          : "border-border hover:bg-muted/50"
      } ${disabled ? "cursor-not-allowed opacity-60" : ""}`}
    >
      <span
        className={`mt-0.5 shrink-0 ${
          selected ? "text-primary" : "text-muted-foreground"
        }`}
      >
        {icon}
      </span>
      <span className="min-w-0 flex-1">
        <span className="flex flex-wrap items-center gap-x-2">
          <span className="text-body font-medium">{title}</span>
          <span className="font-mono text-micro text-muted-foreground">
            {identifier}
          </span>
        </span>
        <span className="mt-0.5 block text-caption text-muted-foreground">
          {description}
        </span>
        {disabled && disabledReason && (
          <span className="mt-1.5 flex items-start gap-1.5 text-caption text-warning">
            <TriangleAlert className="size-3 mt-0.5 shrink-0" />
            <span>{disabledReason}</span>
          </span>
        )}
      </span>
    </button>
  );
}
