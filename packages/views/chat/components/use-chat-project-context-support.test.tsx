import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { useChatProjectContextSupport } from "./use-chat-project-context-support";

vi.mock("@multica/core/api", () => ({
  api: {
    listRuntimes: vi.fn().mockResolvedValue([]),
  },
}));

const WS_ID = "ws-1";

function runtimeRow(cliVersion: string | undefined) {
  return {
    id: "runtime-1",
    workspace_id: WS_ID,
    provider: "claude",
    metadata: cliVersion === undefined ? {} : { cli_version: cliVersion },
  };
}

function renderSupport(agent: { runtime_id?: string | null } | null) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  );
  return renderHook(() => useChatProjectContextSupport(WS_ID, agent), { wrapper });
}

describe("useChatProjectContextSupport", () => {
  beforeEach(() => {
    vi.mocked(api.listRuntimes).mockReset().mockResolvedValue([]);
  });

  it("returns false when the bound runtime reports a stale release version", async () => {
    vi.mocked(api.listRuntimes).mockResolvedValue([runtimeRow("v0.4.9")] as never);

    const { result } = renderSupport({ runtime_id: "runtime-1" });

    await waitFor(() => expect(result.current).toBe(false));
  });

  it("returns true for a new-enough release and for dev-describe builds", async () => {
    vi.mocked(api.listRuntimes).mockResolvedValue([
      runtimeRow("v0.4.10-3-gabc1234"),
    ] as never);

    const { result } = renderSupport({ runtime_id: "runtime-1" });

    await waitFor(() => expect(result.current).toBe(true));
  });

  it("returns null (no warning) when the agent, runtime binding, or runtime row is unknown", async () => {
    vi.mocked(api.listRuntimes).mockResolvedValue([runtimeRow("v0.4.9")] as never);

    expect(renderSupport(null).result.current).toBeNull();
    expect(renderSupport({ runtime_id: null }).result.current).toBeNull();

    const { result } = renderSupport({ runtime_id: "runtime-other" });
    // The list resolves but never contains this runtime — stays "cannot tell".
    await waitFor(() => expect(vi.mocked(api.listRuntimes)).toHaveBeenCalled());
    expect(result.current).toBeNull();
  });

  it("fails closed to false when the runtime row reports no cli_version", async () => {
    vi.mocked(api.listRuntimes).mockResolvedValue([runtimeRow(undefined)] as never);

    const { result } = renderSupport({ runtime_id: "runtime-1" });

    await waitFor(() => expect(result.current).toBe(false));
  });
});
