import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

const mockCerebroRequest = vi.hoisted(() => vi.fn());
const mockListVaults = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/api")>(
    "@multica/core/api",
  );
  return {
    ...actual,
    api: {
      cerebroRequest: mockCerebroRequest,
      listCerebroAgentVaultVaults: mockListVaults,
    },
  };
});

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

import { CredentialVaultPicker } from "./credential-vault-picker";

function renderPicker() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={qc}>
      <CredentialVaultPicker
        wsId="ws-1"
        agentId="agent-1"
        runtimeId="runtime-1"
        userId="user-1"
      />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  mockListVaults.mockResolvedValue({
    vaults: [
      { id: "v1", name: "bigquery" },
      { id: "v2", name: "cloudflare" },
    ],
  });
  // tool-policy chain query: no authored credential grants yet.
  mockCerebroRequest.mockResolvedValue({ tools: [] });
});

describe("CredentialVaultPicker", () => {
  it("lists the live Agent Vault boxes as toggles", async () => {
    renderPicker();
    expect(await screen.findByText("bigquery")).toBeInTheDocument();
    expect(screen.getByText("cloudflare")).toBeInTheDocument();
  });

  it("granting a vault writes credential.reveal Allow on agentvault-vault:<name> at the agent layer", async () => {
    renderPicker();
    const toggle = await screen.findByRole("switch", { name: /Grant bigquery/i });
    await userEvent.click(toggle);

    await waitFor(() => {
      const putCall = mockCerebroRequest.mock.calls.find(
        ([url, opts]) =>
          String(url).endsWith("/tool-policy") && opts?.method === "PUT",
      );
      expect(putCall).toBeDefined();
      const body = JSON.parse(String(putCall?.[1]?.body));
      expect(body).toMatchObject({
        tool_key: "credential.reveal",
        layer: "agent",
        subject_id: "agent-1",
        setting: "allow",
        resource_pattern: "agentvault-vault:bigquery",
      });
    });
  });

  it("reflects an existing grant as an on toggle and clears it on removal", async () => {
    mockCerebroRequest.mockImplementation((url: string, opts?: { method?: string }) => {
      if (String(url).includes("/tool-policy?") && (!opts || !opts.method)) {
        return Promise.resolve({
          tools: [
            {
              tool_key: "credential.reveal",
              resource_pattern: "agentvault-vault:bigquery",
              title: "Reveal credential",
              category: "credentials",
              source: "credential",
              managed_externally: false,
              layers: {
                workspace: null,
                runtime: null,
                agent: "allow",
                group: null,
                user: null,
                system: null,
              },
              conditions: {
                workspace: null,
                runtime: null,
                agent: null,
                user: null,
                system: null,
              },
              effective: { setting: "allow", decided_by: "agent", capped_by: "", reason: "" },
              capped_by_groups: [],
            },
          ],
        });
      }
      return Promise.resolve(undefined);
    });

    renderPicker();
    const toggle = await screen.findByRole("switch", { name: /Grant bigquery/i });
    await waitFor(() => expect(toggle).toBeChecked());

    await userEvent.click(toggle);
    await waitFor(() => {
      const delCall = mockCerebroRequest.mock.calls.find(
        ([url, opts]) =>
          String(url).includes("/tool-policy?") && opts?.method === "DELETE",
      );
      expect(delCall).toBeDefined();
      expect(String(delCall?.[0])).toContain(
        encodeURIComponent("agentvault-vault:bigquery"),
      );
    });
  });
});
