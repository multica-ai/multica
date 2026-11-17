import type { ReactNode } from "react";
import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import type { DingTalkGroup } from "@multica/core/types";
import enSettings from "../../locales/en/settings.json";
import { DingTalkBotGroups } from "./dingtalk-bot-groups";
const groupsRef = {
  current: {
    data: {
      groups: [] as unknown[],
      group_discovery_supported: true,
      inactive_group_counts: {} as Record<string, number>,
    },
  },
};
const inactiveGroupsRef = vi.hoisted(() => ({
  current: {
    data: undefined as
      | undefined
      | { pages: Array<{ groups: unknown[]; next_offset: number | null }> },
    isLoading: false,
    isError: false,
    refetch: vi.fn(),
    hasNextPage: false,
    isFetchingNextPage: false,
    fetchNextPage: vi.fn(),
  },
}));
vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({ invalidateQueries: vi.fn() }),
  useInfiniteQuery: () => inactiveGroupsRef.current,
}));
function GroupList() {
  return (
    <DingTalkBotGroups
      workspaceId="workspace-1"
      installationId="i1"
      groups={groupsRef.current.data.groups as DingTalkGroup[]}
      inactiveCount={groupsRef.current.data.inactive_group_counts?.i1 ?? 0}
      isLoading={false}
      isError={false}
      onRetry={vi.fn()}
    />
  );
}
function renderUI(children: ReactNode) {
  return render(
    <I18nProvider locale="en" resources={{ en: { settings: enSettings } }}>
      {children}
    </I18nProvider>,
  );
}
function resetFixtures() {
  vi.clearAllMocks();
  groupsRef.current.data = {
    groups: [],
    group_discovery_supported: true,
    inactive_group_counts: {},
  };
  inactiveGroupsRef.current.data = undefined;
}

describe("DingTalkBotGroups", () => {
  beforeEach(resetFixtures);
  it("sorts active groups by recency, then inactive groups by title and conversation ID", () => {
    groupsRef.current.data.groups = [
      {
        conversation_id: "cid-alpha-b",
        conversation_title: "Alpha",
        bots: [{ installation_id: "i1", agent_id: "agent-7" }],
      },
      {
        conversation_id: "cid-old",
        conversation_title: "Zulu",
        bots: [
          {
            installation_id: "i1",
            agent_id: "agent-7",
            last_active_at: "2026-08-19T08:00:00Z",
          },
        ],
      },
      {
        conversation_id: "cid-untitled",
        conversation_title: "",
        bots: [{ installation_id: "i1", agent_id: "agent-7" }],
      },
      {
        conversation_id: "cid-recent",
        conversation_title: "Beta",
        bots: [
          {
            installation_id: "i1",
            agent_id: "agent-7",
            last_active_at: "2026-08-19T09:00:00Z",
          },
        ],
      },
      {
        conversation_id: "cid-alpha-a",
        conversation_title: "Alpha",
        bots: [{ installation_id: "i1", agent_id: "agent-7" }],
      },
    ];

    renderUI(<GroupList />);
    expect(
      screen
        .getAllByTestId("dingtalk-group-item")
        .map((item) => item.querySelector("code")?.textContent),
    ).toEqual([
      "cid-recent",
      "cid-old",
      "cid-alpha-a",
      "cid-alpha-b",
      "cid-untitled",
    ]);
  });

  it("globally sorts inactive groups after merging loaded pages", async () => {
    groupsRef.current.data = {
      groups: [],
      group_discovery_supported: true,
      inactive_group_counts: { i1: 4 },
    };
    inactiveGroupsRef.current.data = {
      pages: [
        {
          groups: [
            {
              conversation_id: "cid-zulu",
              conversation_title: "Zulu",
              bots: [{ installation_id: "i1", agent_id: "agent-7" }],
            },
            {
              conversation_id: "cid-bravo-b",
              conversation_title: "Bravo",
              bots: [{ installation_id: "i1", agent_id: "agent-7" }],
            },
          ],
          next_offset: 2,
        },
        {
          groups: [
            {
              conversation_id: "cid-alpha",
              conversation_title: "Alpha",
              bots: [{ installation_id: "i1", agent_id: "agent-7" }],
            },
            {
              conversation_id: "cid-bravo-a",
              conversation_title: "Bravo",
              bots: [{ installation_id: "i1", agent_id: "agent-7" }],
            },
          ],
          next_offset: null,
        },
      ],
    };

    renderUI(<GroupList />);
    await userEvent.click(
      screen.getByRole("button", { name: /long inactive/i }),
    );
    expect(
      screen
        .getAllByTestId("dingtalk-group-item")
        .map((item) => item.querySelector("code")?.textContent),
    ).toEqual(["cid-alpha", "cid-bravo-a", "cid-bravo-b", "cid-zulu"]);
  });
});
