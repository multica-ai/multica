import { useState, type ReactNode } from "react";
import { QueryClient, QueryClientProvider, useQuery } from "@tanstack/react-query";
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import { api } from "@multica/core/api";
import { workspaceListOptions } from "@multica/core/workspace/queries";
import type { Agent, MemberRole, Workspace } from "@multica/core/types";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";
import { WorkspaceDefaultChatAgent } from "./workspace-default-chat-agent";

vi.mock("@multica/core/api", () => ({ api: { listWorkspaces: vi.fn(), listAgents: vi.fn(), updateWorkspace: vi.fn() } }));
vi.mock("sonner", () => ({ toast: { error: vi.fn() } }));
const id = "6cf8d8f8-a688-4023-bd69-e0354f32ce31";
const agent = { id, name: "Chosen agent", owner_id: "user", permission_mode: "private", runtime_id: "runtime" } as Agent;
const base = { id: "ws", name: "Test", slug: "test", settings: { keep: { nested: true } } } as unknown as Workspace;
let qc: QueryClient;
afterEach(() => { cleanup(); qc?.clear(); vi.restoreAllMocks(); });

function setup(role: MemberRole = "owner", hiddenDefault = false) {
  vi.clearAllMocks();
  let stored = structuredClone(base);
  if (hiddenDefault) stored.settings.default_chat_agent_id = "private-other";
  vi.spyOn(api, "listWorkspaces").mockImplementation(async () => {
    const projected = structuredClone(stored);
    if (projected.settings.default_chat_agent_id === "private-other") delete projected.settings.default_chat_agent_id;
    return [projected];
  });
  vi.spyOn(api, "listAgents").mockResolvedValue([
    agent,
    { ...agent, id: "private-other", name: "Private teammate", owner_id: "someone" },
    { ...agent, id: "archived", name: "Archived", archived_at: "now" },
    { ...agent, id: "unbound", name: "Unbound", runtime_id: "" },
  ]);
  const update = vi.spyOn(api, "updateWorkspace").mockImplementation(async (_ws, payload) => {
    stored = { ...stored, settings: { ...stored.settings } };
    if (payload.default_chat_agent_id) stored.settings.default_chat_agent_id = payload.default_chat_agent_id;
    else delete stored.settings.default_chat_agent_id;
    return structuredClone(stored);
  });
  qc = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={qc}><I18nProvider locale="en" resources={{ en: { common: enCommon, settings: enSettings } }}>{children}</I18nProvider></QueryClientProvider>;
  }
  function TestPage() {
    const { data } = useQuery(workspaceListOptions());
    const [draft, setDraft] = useState("unsaved work");
    return <><input aria-label="Unrelated draft" value={draft} onChange={(e) => setDraft(e.target.value)} />{data?.[0] && <WorkspaceDefaultChatAgent workspace={data[0]} userId="user" role={role} />}</>;
  }
  return { update, Wrapper, TestPage, stored: () => stored };
}

describe("workspace default chat agent settings integration", () => {
  it("selects, reloads and explicitly clears using only the top-level field", async () => {
    const user = userEvent.setup();
    const { update, Wrapper, TestPage, stored } = setup();
    const view = render(<TestPage />, { wrapper: Wrapper });
    const selector = await screen.findByRole("combobox", { name: "Default chat agent" });
    await waitFor(() => expect(selector).toBeEnabled());
    await user.click(selector);
    expect(screen.queryByRole("option", { name: "Private teammate" })).not.toBeInTheDocument();
    expect(screen.queryByRole("option", { name: "Archived" })).not.toBeInTheDocument();
    expect(screen.queryByRole("option", { name: "Unbound" })).not.toBeInTheDocument();
    await user.click(screen.getByRole("option", { name: "Chosen agent" }));
    await waitFor(() => expect(update).toHaveBeenCalledWith("ws", { default_chat_agent_id: id }));
    await waitFor(() => expect(selector).toHaveTextContent("Chosen agent"));
    expect(screen.getByLabelText("Unrelated draft")).toHaveValue("unsaved work");
    expect(stored().settings.keep).toEqual({ nested: true });
    // A fresh observer reads persisted server state after cache removal.
    view.unmount();
    act(() => qc.removeQueries());
    render(<TestPage />, { wrapper: Wrapper });
    const reloaded = await screen.findByRole("combobox", { name: "Default chat agent" });
    await waitFor(() => expect(reloaded).toHaveTextContent("Chosen agent"));
    await user.click(screen.getByRole("button", { name: "Clear default" }));
    await waitFor(() => expect(update).toHaveBeenLastCalledWith("ws", { default_chat_agent_id: null }));
    await waitFor(() => expect(reloaded).toHaveTextContent("No workspace default"));
  });
  it("disables editing for members", async () => {
    const { Wrapper, TestPage, update } = setup("member");
    render(<TestPage />, { wrapper: Wrapper });
    expect(await screen.findByRole("combobox", { name: "Default chat agent" })).toBeDisabled();
    expect(update).not.toHaveBeenCalled();
  });
  it("lets an admin explicitly clear a hidden private default without learning its ID", async () => {
    const user = userEvent.setup();
    const { Wrapper, TestPage, update, stored } = setup("admin", true);
    render(<TestPage />, { wrapper: Wrapper });
    const selector = await screen.findByRole("combobox", { name: "Default chat agent" });
    await waitFor(() => expect(selector).toHaveTextContent("No workspace default"));
    expect(stored().settings.default_chat_agent_id).toBe("private-other");
    await user.click(screen.getByRole("button", { name: "Clear default" }));
    await waitFor(() => expect(update).toHaveBeenCalledWith("ws", { default_chat_agent_id: null }));
    expect(stored().settings.default_chat_agent_id).toBeUndefined();
  });

});
