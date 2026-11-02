// @vitest-environment jsdom

import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import type { AgentPresenceDetail } from "@multica/core/agents";
import type { Agent } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enAgents from "../../locales/en/agents.json";
import { AgentFolderAvatars } from "./agent-folder-avatars";

vi.mock("@multica/core/workspace/avatar-url", () => ({ resolvePublicFileUrl: (url: string | null) => url }));

const agents = [
  { id: "image", name: "Image agent", avatar_url: "/api/avatars/sig/avatar.png" },
  { id: "emoji", name: "Emoji agent", avatar_url: "emoji:🚀" },
  { id: "default", name: "Default agent", avatar_url: null },
  { id: "fourth", name: "Fourth agent", avatar_url: null },
  { id: "fifth", name: "Fifth agent", avatar_url: null },
  { id: "sixth", name: "Sixth agent", avatar_url: null },
  { id: "seventh", name: "Seventh agent", avatar_url: null },
] as Agent[];

function presence(availability: AgentPresenceDetail["availability"], workload: AgentPresenceDetail["workload"]): AgentPresenceDetail {
  return { availability, workload, runningCount: workload === "working" ? 1 : 0, queuedCount: workload === "queued" ? 1 : 0, capacity: 2 };
}

describe("AgentFolderAvatars", () => {
  it("uses image, emoji and fallback avatars and caps the preview at four with one overflow badge", () => {
    render(
      <I18nProvider locale="en" resources={{ en: { agents: enAgents } }}>
        <AgentFolderAvatars agents={agents} count={7} emptyLabel="Empty" />
      </I18nProvider>,
    );
    const image = screen.getByRole("img", { name: "Image agent" });
    expect(image.getAttribute("src")).toBe("/api/avatars/sig/avatar.png");
    expect(screen.getByRole("img", { name: "Emoji agent" }).textContent).toBe("🚀");
    expect(screen.getByTitle("Default agent").querySelector("svg")).toBeTruthy();
    expect(screen.getByTitle("Fourth agent").className).not.toContain("hidden");
    expect(screen.queryByTitle("Fifth agent")).toBeNull();
    expect(screen.queryByText("+4")).toBeNull();
    expect(screen.getByText("+3").className).toContain("inline-flex");
    fireEvent.error(image);
    expect(screen.queryByRole("img", { name: "Image agent" })).toBeNull();
    expect(screen.getByTitle("Image agent").querySelector("svg")).toBeTruthy();
  });

  it("omits overflow for three members and shows only the empty badge for zero", () => {
    const { rerender } = render(
      <I18nProvider locale="en" resources={{ en: { agents: enAgents } }}>
        <AgentFolderAvatars agents={agents.slice(0, 3)} count={3} emptyLabel="Empty" />
      </I18nProvider>,
    );
    expect(screen.queryByText(/^\+/)).toBeNull();
    rerender(
      <I18nProvider locale="en" resources={{ en: { agents: enAgents } }}>
        <AgentFolderAvatars agents={[]} count={0} emptyLabel="Empty" />
      </I18nProvider>,
    );
    expect(screen.getByText("Empty").className).toContain("border-dashed");
    expect(screen.queryByTitle("Image agent")).toBeNull();
  });

  it("puts working members before the leader and online members, with presence dots", () => {
    const presenceByAgentId = new Map([
      ["image", presence("online", "idle")],
      ["emoji", presence("offline", "working")],
      ["default", presence("offline", "idle")],
      ["fourth", presence("online", "idle")],
    ]);
    const { container } = render(
      <I18nProvider locale="en" resources={{ en: { agents: enAgents } }}>
        <AgentFolderAvatars
          agents={agents.slice(0, 4)}
          count={4}
          emptyLabel="Empty"
          presenceByAgentId={presenceByAgentId}
          leaderId="fourth"
        />
      </I18nProvider>,
    );
    expect([...container.querySelectorAll("span[title]")].map((element) => element.getAttribute("title"))).toEqual([
      "Emoji agent",
      "Fourth agent",
      "Image agent",
      "Default agent",
    ]);
    expect(screen.getAllByLabelText(/^Status:/)).toHaveLength(4);
    expect(container.querySelectorAll(".bg-success")).toHaveLength(2);
    expect(container.querySelectorAll(".bg-muted-foreground\\/40")).toHaveLength(2);
  });
});
