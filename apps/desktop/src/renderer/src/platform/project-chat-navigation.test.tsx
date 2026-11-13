import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, fireEvent, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { WorkspaceSlugProvider } from "@multica/core/paths";
import { bindWorkspaceSlugLookup } from "@multica/core/query-client";
import { workspaceKeys } from "@multica/core/workspace/queries";
import { chatKeys } from "@multica/core/chat/queries";
import { AppLink, useNavigation } from "@multica/views/navigation";
import { DesktopNavigationProvider } from "./navigation";
import { useTabStore, getActiveTab } from "../stores/tab-store";
import { ProjectChatsSection } from "../../../../../../packages/views/projects/components/project-chats-section";
import { renderWithI18n } from "../../../../../../packages/views/test/i18n";

let client: QueryClient;
let unbind = () => {};
const projectPath = "/acme/projects/project-alpha";
const selectedChatPath = "/acme/chat?session=selected-chat";
const otherChatPath = "/acme/chat?session=other-project-chat&filter=kept#message";

function group() {
  return useTabStore.getState().byWorkspace.acme;
}

function SessionSelection() {
  const navigation = useNavigation();
  return (
    <output data-testid="selected-session">
      {navigation.searchParams.get("session") ?? "none"}
    </output>
  );
}

function mount() {
  return renderWithI18n(
    <QueryClientProvider client={client}>
      <WorkspaceSlugProvider slug="acme">
        <DesktopNavigationProvider>
          <ProjectChatsSection projectId="project-alpha" />
          <SessionSelection />
          <AppLink href={projectPath}>Project Alpha</AppLink>
        </DesktopNavigationProvider>
      </WorkspaceSlugProvider>
    </QueryClientProvider>,
  );
}

function backgroundClick(link: HTMLElement, gesture: "cmd" | "middle") {
  act(() => {
    if (gesture === "cmd") {
      fireEvent.click(link, { metaKey: true });
    } else {
      fireEvent(link, new MouseEvent("auxclick", {
        button: 1,
        bubbles: true,
        cancelable: true,
      }));
    }
  });
}

beforeEach(() => {
  client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  client.setQueryData(workspaceKeys.list(), [
    { id: "ws-alpha", slug: "acme", name: "Acme" },
  ]);
  client.setQueryData(chatKeys.sessions("ws-alpha"), [{
    id: "selected-chat",
    title: "Selected project conversation",
    workspace_id: "ws-alpha",
    creator_id: "fixture-user",
    agent_id: "fixture-agent",
    project_id: "project-alpha",
    status: "active",
    has_unread: false,
    created_at: "2026-10-01T00:00:00Z",
    updated_at: "2026-10-02T00:00:00Z",
  }]);
  unbind = bindWorkspaceSlugLookup(client);
  Object.defineProperty(window, "desktopAPI", {
    configurable: true,
    value: {
      runtimeConfig: { ok: true, config: { appUrl: "https://fixture.example" } },
    },
  });
  useTabStore.getState().reset();
  useTabStore.getState().switchWorkspace("acme", projectPath);
});

afterEach(() => {
  cleanup();
  client.clear();
  unbind();
  useTabStore.getState().reset();
});

describe("project Chats through the Desktop adapter", () => {
  it.each(["cmd", "middle"] as const)(
    "same chat %s background open preserves source, history and target",
    (gesture) => {
      const store = useTabStore.getState();
      const target = store.openTab(selectedChatPath, "Existing chat", { activate: true });
      const retainedUrl = selectedChatPath + "&filter=kept#anchor";
      store.navigateActiveSession(retainedUrl, { replace: true });
      const source = group().tabs.find(tab => tab.url === projectPath)!;
      store.setActiveTab(source.id);
      const before = group();
      mount();
      backgroundClick(screen.getByRole("link", { name: /Selected project conversation/ }), gesture);
      expect(group()).toBe(before);
      expect(getActiveTab(useTabStore.getState())?.id).toBe(source.id);
      expect(useTabStore.getState().tabAttention?.tabId).toBe(target);
      expect(group().tabs).toHaveLength(2);
      expect(group().tabs.find(tab => tab.id === target)?.url).toBe(retainedUrl);
      expect(screen.getByTestId("selected-session")).toHaveTextContent("none");
    },
  );

  it("same chat foreground modifier activates the existing tab and retains its view state", () => {
    const store = useTabStore.getState();
    const retainedUrl = selectedChatPath + "&filter=kept#anchor";
    const target = store.openTab(retainedUrl, "Existing chat");
    const before = group().tabs.find(tab => tab.id === target)!;
    mount();
    fireEvent.click(screen.getByRole("link", { name: /Selected project conversation/ }), {
      metaKey: true,
      shiftKey: true,
    });
    expect(getActiveTab(useTabStore.getState())).toBe(before);
    expect(getActiveTab(useTabStore.getState())?.url).toBe(retainedUrl);
    expect(screen.getByTestId("selected-session")).toHaveTextContent("selected-chat");
    expect(group().tabs).toHaveLength(2);
  });

  it.each(["cmd", "middle"] as const)(
    "project %s background dedupe keeps active chat and project view state",
    (gesture) => {
      const store = useTabStore.getState();
      const retainedUrl = projectPath + "?tab=resources#retained";
      store.navigateActiveSession(retainedUrl, { replace: true });
      const project = group().activeTabId;
      store.openTab(selectedChatPath, "Selected chat", { activate: true });
      const before = group();
      mount();
      backgroundClick(screen.getByRole("link", { name: "Project Alpha" }), gesture);
      expect(group()).toBe(before);
      expect(getActiveTab(useTabStore.getState())?.url).toBe(selectedChatPath);
      expect(useTabStore.getState().tabAttention?.tabId).toBe(project);
      expect(group().tabs.find(tab => tab.id === project)?.url).toBe(retainedUrl);
    },
  );

  it("plain chat navigation opens the selected conversation in place", () => {
    const source = group().activeTabId;
    mount();
    fireEvent.click(screen.getByRole("link", { name: /Selected project conversation/ }));
    expect(getActiveTab(useTabStore.getState())?.url).toBe(selectedChatPath);
    expect(group().activeTabId).toBe(source);
    expect(group().tabs).toHaveLength(1);
    expect(screen.getByTestId("selected-session")).toHaveTextContent("selected-chat");
  });

  it("foreground project chat open selects the requested conversation when another conversation tab exists", () => {
    const store = useTabStore.getState();
    const other = store.openTab(otherChatPath, "Other conversation");
    const before = group().tabs.find(tab => tab.id === other)!;
    mount();
    fireEvent.click(screen.getByRole("link", { name: /Selected project conversation/ }), {
      metaKey: true,
      shiftKey: true,
    });
    expect(getActiveTab(useTabStore.getState())?.url).toBe(selectedChatPath);
    expect(screen.getByTestId("selected-session")).toHaveTextContent("selected-chat");
    expect(group().tabs.find(tab => tab.id === other)).toBe(before);
    expect(group().tabs).toHaveLength(3);
  });

  it("plain open from a pinned project selects the requested conversation when another conversation tab exists", () => {
    const store = useTabStore.getState();
    const project = group().activeTabId;
    store.togglePin(project);
    const other = store.openTab(otherChatPath, "Other conversation");
    const before = group().tabs.find(tab => tab.id === other)!;
    mount();
    fireEvent.click(screen.getByRole("link", { name: /Selected project conversation/ }));
    expect(getActiveTab(useTabStore.getState())?.url).toBe(selectedChatPath);
    expect(screen.getByTestId("selected-session")).toHaveTextContent("selected-chat");
    expect(group().tabs.find(tab => tab.id === project)?.pinned).toBe(true);
    expect(group().tabs.find(tab => tab.id === project)?.url).toBe(projectPath);
    expect(group().tabs.find(tab => tab.id === other)).toBe(before);
    expect(group().tabs).toHaveLength(3);
  });

  it.each(["cmd", "middle"] as const)(
    "%s opens a different session in the background and reuses it on repeat",
    (gesture) => {
      const store = useTabStore.getState();
      const other = store.openTab(otherChatPath, "Other conversation");
      const source = group().activeTabId;
      const otherBefore = group().tabs.find(tab => tab.id === other)!;
      mount();
      const link = screen.getByRole("link", { name: /Selected project conversation/ });
      backgroundClick(link, gesture);
      const target = group().tabs.find(tab => tab.url === selectedChatPath)!;
      expect(target).toBeDefined();
      expect(group().activeTabId).toBe(source);
      expect(screen.getByTestId("selected-session")).toHaveTextContent("none");
      expect(group().tabs.find(tab => tab.id === other)).toBe(otherBefore);
      expect(group().tabs).toHaveLength(3);
      backgroundClick(link, gesture);
      expect(group().tabs).toHaveLength(3);
      expect(group().activeTabId).toBe(source);
      expect(useTabStore.getState().tabAttention?.tabId).toBe(target.id);
      const attention = useTabStore.getState().tabAttention!.sequence;
      backgroundClick(link, gesture);
      expect(useTabStore.getState().tabAttention?.sequence).toBeGreaterThan(attention);
      expect(group().activeTabId).toBe(source);
      fireEvent.click(link, { metaKey: true, shiftKey: true });
      expect(group().activeTabId).toBe(target.id);
      expect(screen.getByTestId("selected-session")).toHaveTextContent("selected-chat");
      expect(group().tabs).toHaveLength(3);
    },
  );

  it("ordinary navigation keeps the active unpinned tab and supports back/forward between sessions", () => {
    const store = useTabStore.getState();
    store.navigateActiveSession(otherChatPath);
    const source = group().activeTabId;
    mount();
    fireEvent.click(screen.getByRole("link", { name: /Selected project conversation/ }));
    expect(group().activeTabId).toBe(source);
    expect(group().tabs).toHaveLength(1);
    expect(screen.getByTestId("selected-session")).toHaveTextContent("selected-chat");
    act(() => store.goBack());
    expect(getActiveTab(useTabStore.getState())?.url).toBe(otherChatPath);
    expect(screen.getByTestId("selected-session")).toHaveTextContent("other-project-chat");
    act(() => store.goForward());
    expect(getActiveTab(useTabStore.getState())?.url).toBe(selectedChatPath);
    expect(screen.getByTestId("selected-session")).toHaveTextContent("selected-chat");
  });
});
