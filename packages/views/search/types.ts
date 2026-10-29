import type { ComponentType, ReactNode } from "react";
import type { ShortcutChord } from "@multica/core/shortcuts";
import type { Agent, ChatSession, Issue, Project, Squad } from "@multica/core/types";

/** The issues an issue command acts on: the open issue, or a list selection. */
export type IssueTarget =
  | { kind: "single"; issue: Issue }
  | { kind: "batch"; issues: Issue[]; clearSelection: () => void };

export type DelegateActor =
  | { type: "agent"; id: string; name: string; agent: Agent }
  | { type: "squad"; id: string; name: string; squad: Squad };

/** A submenu the palette can push onto its page stack. */
export type PalettePage =
  | { type: "issue-status"; target: IssueTarget }
  | { type: "issue-assignee"; target: IssueTarget }
  | { type: "issue-priority"; target: IssueTarget }
  | { type: "issue-labels"; target: IssueTarget }
  | { type: "issue-due-date"; target: IssueTarget }
  | { type: "issue-project"; target: IssueTarget }
  | { type: "issue-parent"; target: IssueTarget }
  | { type: "issue-quick-action"; issue: Issue }
  | { type: "theme" }
  | { type: "project-status"; project: Project }
  | { type: "agent-assign-issue"; agent: Agent }
  | { type: "chat-project"; session: ChatSession; changeProject: (projectId: string | null) => void }
  | { type: "delegate-target"; prompt: string };

export interface PaletteStackEntry {
  page: PalettePage;
  /** Breadcrumb segments shown above the input, e.g. ["MUL-12", "Assign to"]. */
  crumbs: string[];
}

export type PaletteIcon = ComponentType<{ className?: string }>;

export interface PaletteCommand {
  id: string;
  label: string;
  icon: PaletteIcon;
  /** Hidden aliases, prefix-matched. */
  keywords?: string[];
  shortcut?: ShortcutChord | null;
  /** Muted text on the right, e.g. "3 available". */
  hint?: string;
  trailing?: ReactNode;
  /** Opens a submenu instead of running. */
  page?: PalettePage;
  /** Breadcrumb subject for `page`, e.g. the issue identifier. */
  crumb?: string;
  run?: () => void;
  /** Shown in the empty state; the rest appear once the user types. */
  primary?: boolean;
  destructive?: boolean;
}

export interface PaletteCommandGroup {
  id: string;
  heading: ReactNode;
  commands: PaletteCommand[];
}

/** Dialogs the palette opens after it closes itself. */
export type PaletteHostDialog =
  | { type: "cancel-run"; issueId: string; taskId: string; running: boolean }
  | { type: "batch-delete"; issues: Issue[]; clearSelection: () => void }
  | { type: "new-autopilot" };
