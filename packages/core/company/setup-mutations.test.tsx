// @vitest-environment jsdom
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ChatPinnedAgent } from "../types";
import { useSetupCompany } from "./setup-mutations";
import { companySetupKeys } from "./setup-query";
import type { CompanySetupReadiness } from "./setup-schema";

const api = vi.hoisted(() => ({
  setupCompany: vi.fn(),
  pinChatAgent: vi.fn(),
  listChatPinnedAgents: vi.fn(),
}));

vi.mock("../api", () => ({ api }));

const ready: CompanySetupReadiness = {
  runtime_online: true,
  secretary_id: "sec-1",
  secretary_desk_project_id: "desk-1",
  executive_floor: true,
  autopilot_daily: true,
  autopilot_weekly_paused: true,
  secretary_instructions_current: true,
  ready: true,
};

describe("useSetupCompany", () => {
  let pinned: ChatPinnedAgent[];
  let queryClient: QueryClient;

  beforeEach(() => {
    vi.clearAllMocks();
    pinned = [];
    api.setupCompany.mockResolvedValue(ready);
    api.listChatPinnedAgents.mockImplementation(async () => pinned);
    api.pinChatAgent.mockImplementation(async (agentId: string) => {
      pinned = [...pinned, { agent_id: agentId, position: pinned.length + 1 } as ChatPinnedAgent];
      return pinned.at(-1);
    });
    queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  });

  function wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
  }

  function mount() {
    return renderHook(() => useSetupCompany("ws-1"), { wrapper });
  }

  it("pins the Secretary in the viewer's chat after setup", async () => {
    const { result } = mount();
    await act(async () => {
      await result.current.mutateAsync({ secretary_instructions: "text" });
    });
    expect(api.pinChatAgent).toHaveBeenCalledTimes(1);
    expect(api.pinChatAgent).toHaveBeenCalledWith("sec-1");
  });

  it("pins once: running setup again does not pin a second time", async () => {
    const { result } = mount();
    await act(async () => {
      await result.current.mutateAsync({ secretary_instructions: "text" });
    });
    await act(async () => {
      await result.current.mutateAsync({ secretary_instructions: "text" });
    });
    expect(api.setupCompany).toHaveBeenCalledTimes(2);
    expect(api.pinChatAgent).toHaveBeenCalledTimes(1);
  });

  it("does not pin when the viewer already has the Secretary pinned", async () => {
    pinned = [{ agent_id: "sec-1", position: 1 } as ChatPinnedAgent];
    const { result } = mount();
    await act(async () => {
      await result.current.mutateAsync({ secretary_instructions: "text" });
    });
    expect(api.pinChatAgent).not.toHaveBeenCalled();
  });

  it("still succeeds when the pin fails", async () => {
    api.pinChatAgent.mockRejectedValue(new Error("too many pinned agents"));
    const { result } = mount();
    let readiness: CompanySetupReadiness | undefined;
    await act(async () => {
      readiness = await result.current.mutateAsync({ secretary_instructions: "text" });
    });
    expect(readiness).toEqual(ready);
    await waitFor(() => expect(api.pinChatAgent).toHaveBeenCalledTimes(1));
  });

  it("does not pin when setup names no Secretary", async () => {
    api.setupCompany.mockResolvedValue({ ...ready, secretary_id: null, ready: false });
    const { result } = mount();
    await act(async () => {
      await result.current.mutateAsync({ secretary_instructions: "text" });
    });
    expect(api.pinChatAgent).not.toHaveBeenCalled();
  });

  it("leaves the cached readiness alone and rethrows a refusal", async () => {
    const refusal = Object.assign(new Error("runtime required"), {
      body: { code: "runtime_required" },
    });
    api.setupCompany.mockRejectedValueOnce(refusal);
    const { result } = mount();
    await act(async () => {
      await expect(result.current.mutateAsync({ secretary_instructions: "text" })).rejects.toBe(refusal);
    });
    expect(queryClient.getQueryData(companySetupKeys.all("ws-1"))).toBeUndefined();
    expect(api.pinChatAgent).not.toHaveBeenCalled();
  });
});
