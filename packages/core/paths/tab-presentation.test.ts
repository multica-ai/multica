import { describe, it, expect } from "vitest";
import { parseTabSubject } from "./tab-subject";
import {
  resolveTabPresentation,
  iconForAttachment,
  type TabEntityData,
} from "./tab-presentation";

// External-visible contract: (URL, cached data) → (visual, title). Tests the
// combination, not the internal component tree — exactly the surface the PRD
// pins down.
function present(url: string, data: TabEntityData = {}) {
  return resolveTabPresentation(parseTabSubject(url), data);
}

describe("resolveTabPresentation — pages", () => {
  it("uses the page icon and localized page name", () => {
    expect(present("/acme/issues")).toEqual({
      visual: { kind: "icon", icon: "ListTodo" },
      title: { kind: "nav", navKey: "issues" },
    });
    expect(present("/acme/projects")).toEqual({
      visual: { kind: "icon", icon: "FolderKanban" },
      title: { kind: "nav", navKey: "projects" },
    });
  });
});

describe("resolveTabPresentation — direct resources", () => {
  it("issue shows live status + identifier:title, pending shows type label", () => {
    expect(present("/acme/issues/i1")).toEqual({
      visual: { kind: "issue-status", status: null },
      title: { kind: "tab", tabKey: "issue" },
    });
    expect(
      present("/acme/issues/i1", {
        issue: { identifier: "MUL-1", title: "Fix", status: "in_progress" },
      }),
    ).toEqual({
      visual: { kind: "issue-status", status: "in_progress" },
      title: { kind: "text", text: "MUL-1: Fix" },
    });
  });

  it("project shows its own icon + title, pending shows default glyph + label", () => {
    expect(present("/acme/projects/p1")).toEqual({
      visual: { kind: "project-icon", icon: null },
      title: { kind: "tab", tabKey: "project" },
    });
    expect(
      present("/acme/projects/p1", { project: { icon: "🚀", title: "Apollo" } }),
    ).toEqual({
      visual: { kind: "project-icon", icon: "🚀" },
      title: { kind: "text", text: "Apollo" },
    });
  });

  it("actor shows avatar visual and resolved name", () => {
    expect(present("/acme/agents/ag1")).toEqual({
      visual: { kind: "actor", actorType: "agent", id: "ag1" },
      title: { kind: "tab", tabKey: "agent" },
    });
    expect(present("/acme/members/m1", { actorName: "Ada" })).toEqual({
      visual: { kind: "actor", actorType: "member", id: "m1" },
      title: { kind: "text", text: "Ada" },
    });
    expect(present("/acme/squads/sq1")).toEqual({
      visual: { kind: "actor", actorType: "squad", id: "sq1" },
      title: { kind: "tab", tabKey: "squad" },
    });
  });

  it("autopilot / skill / machine / runtime use a type icon + name", () => {
    expect(present("/acme/autopilots/a1", { autopilot: { title: "Nightly" } })).toEqual({
      visual: { kind: "icon", icon: "Zap" },
      title: { kind: "text", text: "Nightly" },
    });
    expect(present("/acme/skills/s1", { skill: { name: "Deploy" } })).toEqual({
      visual: { kind: "icon", icon: "BookOpenText" },
      title: { kind: "text", text: "Deploy" },
    });
    expect(present("/acme/runtimes/m1", { machine: { name: "Mac Studio" } })).toEqual({
      visual: { kind: "icon", icon: "Monitor" },
      title: { kind: "text", text: "Mac Studio" },
    });
    expect(
      present("/acme/runtimes/m1/runtime/r1", { runtime: { name: "cloud-1" } }),
    ).toEqual({
      visual: { kind: "icon", icon: "Server" },
      title: { kind: "text", text: "cloud-1" },
    });
  });

  it("attachment shows the filename and a matching file icon, falling back only when missing", () => {
    // Filename present (from ?name=) → title is the filename, icon matches type.
    expect(present("/acme/attachments/att1/preview?name=report.pdf")).toEqual({
      visual: { kind: "icon", icon: "FileText" },
      title: { kind: "text", text: "report.pdf" },
    });
    expect(present("/acme/attachments/att1/preview?name=diagram.png").visual).toEqual({
      kind: "icon",
      icon: "FileImage",
    });
    expect(present("/acme/attachments/att1/preview?name=app.tsx").visual).toEqual({
      kind: "icon",
      icon: "FileCode",
    });
    // Missing filename → generic File icon + "Attachment" label.
    expect(present("/acme/attachments/att1/preview")).toEqual({
      visual: { kind: "icon", icon: "File" },
      title: { kind: "tab", tabKey: "attachment" },
    });
    // Unknown / extensionless → generic File, but still shows the filename.
    expect(present("/acme/attachments/att1/preview?name=LICENSE")).toEqual({
      visual: { kind: "icon", icon: "File" },
      title: { kind: "text", text: "LICENSE" },
    });
  });

  it("never borrows the Issues icon while a resource is loading", () => {
    // A pending issue uses issue-status (not the ListTodo page icon); a pending
    // project uses project-icon; neither is the generic page fallback.
    expect(present("/acme/issues/i1").visual).toEqual({
      kind: "issue-status",
      status: null,
    });
    expect(present("/acme/projects/p1").visual).toEqual({
      kind: "project-icon",
      icon: null,
    });
  });
});

describe("resolveTabPresentation — containers keep their icon, title tracks selection", () => {
  it("inbox: icon is always Inbox; title is the selected item or Inbox", () => {
    expect(present("/acme/inbox")).toEqual({
      visual: { kind: "icon", icon: "Inbox" },
      title: { kind: "nav", navKey: "inbox" },
    });
    // Selected but not yet resolved → still Inbox, never a stale title.
    expect(present("/acme/inbox?issue=MUL-9")).toEqual({
      visual: { kind: "icon", icon: "Inbox" },
      title: { kind: "nav", navKey: "inbox" },
    });
    // Selected issue → Inbox icon + issue title (distinct from a direct issue,
    // which would show a status icon).
    expect(
      present("/acme/inbox?issue=MUL-9", {
        inboxSelection: { kind: "issue", identifier: "MUL-9", title: "Bug" },
      }),
    ).toEqual({
      visual: { kind: "icon", icon: "Inbox" },
      title: { kind: "text", text: "MUL-9: Bug" },
    });
    // Selected non-issue notification → its display title.
    expect(
      present("/acme/inbox?issue=n1", {
        inboxSelection: { kind: "item", title: "Quick create failed" },
      }),
    ).toEqual({
      visual: { kind: "icon", icon: "Inbox" },
      title: { kind: "text", text: "Quick create failed" },
    });
  });

  it("chat: icon is always MessageSquare; title is the session or Chat", () => {
    expect(present("/acme/chat")).toEqual({
      visual: { kind: "icon", icon: "MessageSquare" },
      title: { kind: "nav", navKey: "chat" },
    });
    // New-chat draft (?agent=) has no session yet → stays Chat.
    expect(present("/acme/chat?agent=ag1")).toEqual({
      visual: { kind: "icon", icon: "MessageSquare" },
      title: { kind: "nav", navKey: "chat" },
    });
    expect(
      present("/acme/chat?session=s1", { chatSessionTitle: "Deploy plan" }),
    ).toEqual({
      visual: { kind: "icon", icon: "MessageSquare" },
      title: { kind: "text", text: "Deploy plan" },
    });
    // Selected session still loading → Chat, not empty.
    expect(present("/acme/chat?session=s1")).toEqual({
      visual: { kind: "icon", icon: "MessageSquare" },
      title: { kind: "nav", navKey: "chat" },
    });
  });
});

describe("resolveTabPresentation — flow and unknown", () => {
  it("create-agent flow uses the agent icon and a flow label", () => {
    expect(present("/acme/agents/new")).toEqual({
      visual: { kind: "icon", icon: "Bot" },
      title: { kind: "tab", tabKey: "create_agent" },
    });
  });

  it("unknown route uses a neutral icon and label, never Issues", () => {
    expect(present("/acme/mystery")).toEqual({
      visual: { kind: "icon", icon: "FileQuestion" },
      title: { kind: "tab", tabKey: "unknown" },
    });
  });
});

describe("iconForAttachment", () => {
  it("maps extensions to file-type icons", () => {
    expect(iconForAttachment("a.png")).toBe("FileImage");
    expect(iconForAttachment("clip.MP4")).toBe("FileVideo");
    expect(iconForAttachment("song.mp3")).toBe("FileAudio");
    expect(iconForAttachment("bundle.zip")).toBe("FileArchive");
    expect(iconForAttachment("main.ts")).toBe("FileCode");
    expect(iconForAttachment("notes.md")).toBe("FileText");
  });

  it("is case-insensitive on the extension", () => {
    expect(iconForAttachment("PHOTO.JPEG")).toBe("FileImage");
  });

  it("falls back to the generic File icon", () => {
    expect(iconForAttachment(null)).toBe("File");
    expect(iconForAttachment("LICENSE")).toBe("File"); // no extension
    expect(iconForAttachment("archive.")).toBe("File"); // trailing dot
    expect(iconForAttachment("data.unknownext")).toBe("File");
  });
});
