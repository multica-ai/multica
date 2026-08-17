/**
 * Pure presentation resolver for desktop tabs.
 *
 * Given a {@link TabSubject} (what the tab points at) and whatever entity data
 * is currently available from cache, produce the tab's leading *visual* and its
 * *title* spec. This is the single place the "what should this tab look like"
 * decision lives — icon and title no longer come from two unrelated code paths.
 *
 * It is pure and React-free: the visual is a descriptor (rendered by
 * `@multica/views`' `ResourceLeadingVisual`) and the title is a spec that is
 * either literal text or a localization key (localized by the view layer).
 * Keeping it pure makes the whole "URL + data → icon + title" matrix unit
 * testable without React, which is exactly what the tab behavior needs guarded.
 *
 * Missing entity data is a first-class state: a resource whose data has not
 * loaded yet renders a stable type icon and a type label, never a wrong or
 * empty identity, and never borrows the Issues icon.
 */
import type { IssueStatus } from "../types";
import {
  WORKSPACE_PAGES,
  type NavLabelKey,
  type RouteIconName,
} from "./route-icons";
import type { TabActorType, TabSubject } from "./tab-subject";

/** The leading visual a tab should render. */
export type TabVisual =
  /** A static Lucide icon (page icon or resourceless type icon). */
  | { kind: "icon"; icon: RouteIconName }
  /** An issue's live status glyph. `null` while the issue is loading. */
  | { kind: "issue-status"; status: IssueStatus | null }
  /** A project's own icon. `null` falls back to the default project glyph. */
  | { kind: "project-icon"; icon: string | null }
  /** An actor's avatar, resolved by the view layer from `actorType`+`id`. */
  | { kind: "actor"; actorType: TabActorType; id: string };

/** Localization keys under the `layout.tab` namespace for tab type labels. */
export type TabLabelKey =
  | "issue"
  | "project"
  | "autopilot"
  | "agent"
  | "member"
  | "squad"
  | "skill"
  | "machine"
  | "runtime"
  | "attachment"
  | "create_agent"
  | "unknown";

/** How a tab's title should be produced. */
export type TabTitleSpec =
  /** Fully resolved literal text (a resource's own name/title). */
  | { kind: "text"; text: string }
  /** A page name — localize via `layout.nav.<navKey>`. */
  | { kind: "nav"; navKey: NavLabelKey }
  /** A type label (loading / flow / unknown) — localize via `layout.tab.<tabKey>`. */
  | { kind: "tab"; tabKey: TabLabelKey };

export interface TabPresentation {
  visual: TabVisual;
  title: TabTitleSpec;
}

/** Resolved inbox selection, as computed by the view layer from cache. */
export type InboxSelectionData =
  | { kind: "issue"; identifier: string; title: string }
  | { kind: "item"; title: string };

/**
 * Entity data the view layer has resolved from cache for the tab's subject.
 * Every field is optional: `undefined` means "not loaded yet" and yields the
 * pending (type-label + type-icon) presentation.
 */
export interface TabEntityData {
  issue?: { identifier: string; title: string; status: IssueStatus };
  project?: { icon: string | null; title: string };
  autopilot?: { title: string };
  /** Resolved display name for an actor subject. */
  actorName?: string;
  skill?: { name: string };
  machine?: { name: string };
  runtime?: { name: string };
  /** Resolved chat session title (already includes the "New chat" fallback). */
  chatSessionTitle?: string;
  /** Resolved inbox selection (issue identifier/title or item title). */
  inboxSelection?: InboxSelectionData;
}

/** Neutral visual used when nothing better can be resolved. */
export const DEFAULT_TAB_VISUAL: TabVisual = { kind: "icon", icon: "FileQuestion" };

/** Literal text if non-empty, otherwise the given type label. */
function textOr(text: string | undefined | null, tabKey: TabLabelKey): TabTitleSpec {
  const trimmed = text?.trim();
  return trimmed ? { kind: "text", text: trimmed } : { kind: "tab", tabKey };
}

const ACTOR_LABEL: Record<TabActorType, TabLabelKey> = {
  agent: "agent",
  member: "member",
  squad: "squad",
};

// Extension → file-type icon. The preview URL only carries the filename, so the
// extension is the available signal; anything unrecognized uses the generic
// File glyph.
const EXTENSION_ICON: Record<string, RouteIconName> = {};
const registerExtensions = (icon: RouteIconName, exts: string[]) => {
  for (const ext of exts) EXTENSION_ICON[ext] = icon;
};
registerExtensions("FileImage", [
  "png", "jpg", "jpeg", "gif", "webp", "svg", "bmp", "ico", "avif", "heic",
]);
registerExtensions("FileVideo", ["mp4", "mov", "webm", "mkv", "avi", "m4v"]);
registerExtensions("FileAudio", ["mp3", "wav", "ogg", "flac", "m4a", "aac"]);
registerExtensions("FileArchive", ["zip", "tar", "gz", "tgz", "rar", "7z", "bz2"]);
registerExtensions("FileCode", [
  "js", "jsx", "ts", "tsx", "json", "yaml", "yml", "py", "go", "rs",
  "java", "c", "h", "cpp", "cc", "rb", "php", "swift", "kt", "sh", "css", "scss",
]);
registerExtensions("FileText", [
  "txt", "md", "markdown", "pdf", "doc", "docx", "csv", "log",
  "html", "htm", "xml", "rtf", "odt",
]);

/** Choose the file icon for an attachment from its filename, else generic File. */
export function iconForAttachment(filename: string | null): RouteIconName {
  if (!filename) return "File";
  const dot = filename.lastIndexOf(".");
  if (dot < 0 || dot === filename.length - 1) return "File";
  const ext = filename.slice(dot + 1).toLowerCase();
  return EXTENSION_ICON[ext] ?? "File";
}

/**
 * Resolve a subject + available data into the tab's leading visual and title.
 * Exhaustive over every {@link TabSubject} kind so a new route/resource type
 * forces an explicit presentation choice rather than a silent default.
 */
export function resolveTabPresentation(
  subject: TabSubject,
  data: TabEntityData = {},
): TabPresentation {
  switch (subject.kind) {
    case "page": {
      const page = WORKSPACE_PAGES[subject.page];
      return {
        visual: { kind: "icon", icon: page.icon },
        title: { kind: "nav", navKey: page.navKey },
      };
    }
    case "issue":
      return {
        visual: { kind: "issue-status", status: data.issue?.status ?? null },
        title: data.issue
          ? { kind: "text", text: `${data.issue.identifier}: ${data.issue.title}` }
          : { kind: "tab", tabKey: "issue" },
      };
    case "project":
      return {
        visual: { kind: "project-icon", icon: data.project?.icon ?? null },
        title: textOr(data.project?.title, "project"),
      };
    case "autopilot":
      return {
        visual: { kind: "icon", icon: "Zap" },
        title: textOr(data.autopilot?.title, "autopilot"),
      };
    case "actor":
      return {
        visual: { kind: "actor", actorType: subject.actorType, id: subject.id },
        title: textOr(data.actorName, ACTOR_LABEL[subject.actorType]),
      };
    case "skill":
      return {
        visual: { kind: "icon", icon: "BookOpenText" },
        title: textOr(data.skill?.name, "skill"),
      };
    case "machine":
      return {
        visual: { kind: "icon", icon: "Monitor" },
        title: textOr(data.machine?.name, "machine"),
      };
    case "runtime":
      return {
        visual: { kind: "icon", icon: "Server" },
        title: textOr(data.runtime?.name, "runtime"),
      };
    case "attachment":
      // The preview URL carries the filename (`?name=`), so use it for the
      // title and pick a matching file icon from its extension. Only fall back
      // to the generic File glyph + "Attachment" label when it's missing.
      return {
        visual: { kind: "icon", icon: iconForAttachment(subject.filename) },
        title: subject.filename
          ? { kind: "text", text: subject.filename }
          : { kind: "tab", tabKey: "attachment" },
      };
    case "inbox": {
      // The container icon never changes; only the title tracks the selection.
      const sel = subject.selectedKey ? data.inboxSelection : undefined;
      let title: TabTitleSpec;
      if (!sel) {
        title = { kind: "nav", navKey: "inbox" };
      } else if (sel.kind === "issue") {
        title = { kind: "text", text: `${sel.identifier}: ${sel.title}` };
      } else {
        const text = sel.title.trim();
        title = text ? { kind: "text", text } : { kind: "nav", navKey: "inbox" };
      }
      return { visual: { kind: "icon", icon: "Inbox" }, title };
    }
    case "chat": {
      const title: TabTitleSpec =
        subject.sessionId && data.chatSessionTitle?.trim()
          ? { kind: "text", text: data.chatSessionTitle.trim() }
          : { kind: "nav", navKey: "chat" };
      return { visual: { kind: "icon", icon: "MessageSquare" }, title };
    }
    case "flow":
      return {
        visual: { kind: "icon", icon: "Bot" },
        title: { kind: "tab", tabKey: "create_agent" },
      };
    case "unknown":
      return {
        visual: DEFAULT_TAB_VISUAL,
        title: { kind: "tab", tabKey: "unknown" },
      };
  }
}
