/**
 * Semantic target resolver for desktop tabs.
 *
 * A tab points at a URL, but what a tab *is* — a collection page, a specific
 * resource, a container with a selected item, a creation flow, or something
 * unrecognized — is what decides how it should look (icon + title). This module
 * turns a URL into that semantic {@link TabSubject}. It is pure: no React, no
 * Lucide, no query — only URL parsing. The visual/title mapping lives in
 * `tab-presentation.ts`; the React glue lives in `@multica/views`.
 *
 * Workspace URLs are `/{slug}/{segment}/...`. The slug is index 0, the route
 * segment index 1, and any resource id index 2. Container selection (Inbox,
 * Chat) rides in the query string, so it is parsed too — the URL is the single
 * source of truth for what a tab has open, including which item.
 */
import { pageForSegment, type WorkspacePageKey } from "./route-icons";

export type TabActorType = "agent" | "member" | "squad";

export type TabSubject =
  /** A collection or tool page with no specific resource. */
  | { kind: "page"; page: WorkspacePageKey }
  /** A single issue detail. */
  | { kind: "issue"; id: string }
  /** A single project detail. */
  | { kind: "project"; id: string }
  /** A single autopilot detail. */
  | { kind: "autopilot"; id: string }
  /** An agent / member / squad detail (has an avatar identity). */
  | { kind: "actor"; actorType: TabActorType; id: string }
  /** A single skill detail. */
  | { kind: "skill"; id: string }
  /** A runtime machine detail. */
  | { kind: "machine"; machineId: string }
  /** A runtime nested under a machine. */
  | { kind: "runtime"; machineId: string; runtimeId: string }
  /** An attachment preview. `filename` is the `?name=` hint, or null. */
  | { kind: "attachment"; id: string; filename: string | null }
  /**
   * The Inbox container; `selectedKey` is the `?issue=` selection or null,
   * `archived` is the `?view=archived` sub-list (its own list/cache).
   */
  | { kind: "inbox"; selectedKey: string | null; archived: boolean }
  /** The Chat container; `sessionId` is the `?session=` selection or null. */
  | { kind: "chat"; sessionId: string | null }
  /** A creation flow that has not produced a resource yet. */
  | { kind: "flow"; flow: "create-agent" }
  /** An unrecognized URL. Never impersonate a real page. */
  | { kind: "unknown" };

/** Split a URL into its `/`-separated pathname segments and its query params. */
function splitUrl(url: string): { segments: string[]; query: URLSearchParams } {
  const hashIdx = url.indexOf("#");
  const withoutHash = hashIdx === -1 ? url : url.slice(0, hashIdx);
  const queryIdx = withoutHash.indexOf("?");
  const pathname = queryIdx === -1 ? withoutHash : withoutHash.slice(0, queryIdx);
  const search = queryIdx === -1 ? "" : withoutHash.slice(queryIdx + 1);
  return {
    segments: pathname.split("/").filter(Boolean),
    query: new URLSearchParams(search),
  };
}

/**
 * Resolve a workspace tab URL into a {@link TabSubject}.
 *
 * Only recognizes workspace-scoped URLs (`/{slug}/{segment}/...`). Anything
 * shorter, empty, or with an unknown segment resolves to `{ kind: "unknown" }`
 * — never to a default page, so an unrecognized tab is visibly neutral rather
 * than masquerading as Issues.
 */
export function parseTabSubject(url: string): TabSubject {
  const { segments, query } = splitUrl(url);
  // segments[0] is the workspace slug; the route segment is at index 1.
  const segment = segments[1] ?? "";
  const id = segments[2] ?? "";

  switch (segment) {
    case "issues":
      return id ? { kind: "issue", id } : { kind: "page", page: "issues" };
    case "my-issues":
      return { kind: "page", page: "myIssues" };
    case "projects":
      return id ? { kind: "project", id } : { kind: "page", page: "projects" };
    case "autopilots":
      return id ? { kind: "autopilot", id } : { kind: "page", page: "autopilots" };
    case "agents":
      if (id === "new") return { kind: "flow", flow: "create-agent" };
      return id
        ? { kind: "actor", actorType: "agent", id }
        : { kind: "page", page: "agents" };
    case "members":
      // No members collection route exists; only `/members/:id`.
      return id ? { kind: "actor", actorType: "member", id } : { kind: "unknown" };
    case "squads":
      return id
        ? { kind: "actor", actorType: "squad", id }
        : { kind: "page", page: "squads" };
    case "usage":
      return { kind: "page", page: "usage" };
    case "inbox":
      return {
        kind: "inbox",
        selectedKey: query.get("issue") || null,
        archived: query.get("view") === "archived",
      };
    case "chat":
      return { kind: "chat", sessionId: query.get("session") || null };
    case "runtimes":
      if (!id) return { kind: "page", page: "runtimes" };
      // `/runtimes/:machineId/runtime/:runtimeId` — nested runtime.
      if (segments[3] === "runtime" && segments[4]) {
        return { kind: "runtime", machineId: id, runtimeId: segments[4] };
      }
      return { kind: "machine", machineId: id };
    case "skills":
      return id ? { kind: "skill", id } : { kind: "page", page: "skills" };
    case "settings":
      return { kind: "page", page: "settings" };
    case "attachments":
      // `/attachments/:id/preview?name=<filename>`.
      return id
        ? { kind: "attachment", id, filename: query.get("name") || null }
        : { kind: "unknown" };
    default: {
      // A bare `/{slug}` (no route segment) is normalized to the default
      // surface elsewhere; treat any other unknown segment as unknown so it
      // does not silently borrow a page's identity.
      const page = pageForSegment(segment);
      return page ? { kind: "page", page } : { kind: "unknown" };
    }
  }
}

/**
 * A stable string identity for a subject, useful as a memo/render key. Distinct
 * subjects that should render the same visual share a key; a selection change
 * inside a container changes it.
 */
export function tabSubjectKey(subject: TabSubject): string {
  switch (subject.kind) {
    case "page":
      return `page:${subject.page}`;
    case "issue":
      return `issue:${subject.id}`;
    case "project":
      return `project:${subject.id}`;
    case "autopilot":
      return `autopilot:${subject.id}`;
    case "actor":
      return `actor:${subject.actorType}:${subject.id}`;
    case "skill":
      return `skill:${subject.id}`;
    case "machine":
      return `machine:${subject.machineId}`;
    case "runtime":
      return `runtime:${subject.machineId}:${subject.runtimeId}`;
    case "attachment":
      return `attachment:${subject.id}:${subject.filename ?? ""}`;
    case "inbox":
      return `inbox:${subject.archived ? "archived" : "inbox"}:${subject.selectedKey ?? ""}`;
    case "chat":
      return `chat:${subject.sessionId ?? ""}`;
    case "flow":
      return `flow:${subject.flow}`;
    case "unknown":
      return "unknown";
  }
}
