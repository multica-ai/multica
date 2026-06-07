// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

type MemberRole = "owner" | "admin" | "member";

const membersRef = vi.hoisted(() => ({
  current: [{ user_id: "user-1", role: "admin" as MemberRole }],
}));
const mockCreateMcpConnector = vi.hoisted(() => vi.fn());
const mockInvalidate = vi.hoisted(() => vi.fn());

vi.mock("@tanstack/react-query", () => ({
  useQuery: (opts: { queryKey: unknown[] }) => {
    const key = JSON.stringify(opts.queryKey);
    if (key.includes("members")) return { data: membersRef.current };
    return { data: undefined };
  },
  queryOptions: <T,>(opts: T) => opts,
  useQueryClient: () => ({ invalidateQueries: mockInvalidate }),
  useMutation: (opts: {
    mutationFn: (vars: unknown) => Promise<unknown>;
    onSuccess?: (data: unknown) => void;
    onError?: (err: unknown) => void;
  }) => ({
    isPending: false,
    mutate: (vars: unknown) => {
      opts
        .mutationFn(vars)
        .then((data) => opts.onSuccess?.(data))
        .catch((err) => opts.onError?.(err));
    },
  }),
}));

vi.mock("@multica/core/auth", () => {
  const useAuthStore = Object.assign(
    (sel?: (s: { user: { id: string } }) => unknown) =>
      sel ? sel({ user: { id: "user-1" } }) : { user: { id: "user-1" } },
    { getState: () => ({ user: { id: "user-1" } }) },
  );
  return { useAuthStore };
});

vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["members"], queryFn: vi.fn() }),
}));

vi.mock("@multica/core/agents", () => ({
  mcpConnectorKeys: {
    list: (wsId: string) => ["workspaces", wsId, "mcp-connectors", "list"],
  },
}));

vi.mock("@multica/core/api", () => ({
  api: {
    createMcpConnector: (...args: unknown[]) => mockCreateMcpConnector(...args),
  },
}));

vi.mock("sonner", () => ({ toast: { error: vi.fn(), success: vi.fn() } }));

import { CustomConnectorEntry } from "./custom-connector-form";

beforeEach(() => {
  mockCreateMcpConnector.mockReset();
  mockCreateMcpConnector.mockResolvedValue({});
  mockInvalidate.mockReset();
  membersRef.current = [{ user_id: "user-1", role: "admin" }];
});

describe("CustomConnectorEntry", () => {
  it("renders the control for an admin", () => {
    render(<CustomConnectorEntry wsId="ws-1" />);
    expect(
      screen.getByRole("button", { name: "Add custom connector" }),
    ).toBeInTheDocument();
  });

  it("renders nothing for a non-admin member", () => {
    membersRef.current = [{ user_id: "user-1", role: "member" }];
    render(<CustomConnectorEntry wsId="ws-1" />);
    expect(
      screen.queryByRole("button", { name: "Add custom connector" }),
    ).not.toBeInTheDocument();
  });

  it("submits createMcpConnector and invalidates the list on success", async () => {
    const user = userEvent.setup();
    render(<CustomConnectorEntry wsId="ws-1" />);

    await user.click(
      screen.getByRole("button", { name: "Add custom connector" }),
    );

    await user.type(screen.getByLabelText(/Name/), "Internal Wiki");
    await user.type(screen.getByLabelText(/Slug/), "internal-wiki");
    // JSON contains `{`/`}` which userEvent.type interprets as key
    // descriptors — set the value directly to avoid escaping.
    fireEvent.change(screen.getByLabelText(/MCP template/), {
      target: {
        value: '{"mcpServers":{"wiki":{"command":"run"}}}',
      },
    });

    await user.click(screen.getByRole("button", { name: "Add connector" }));

    await waitFor(() => {
      expect(mockCreateMcpConnector).toHaveBeenCalledTimes(1);
    });
    expect(mockCreateMcpConnector).toHaveBeenCalledWith("ws-1", {
      slug: "internal-wiki",
      name: "Internal Wiki",
      description: null,
      mcp_template: { mcpServers: { wiki: { command: "run" } } },
    });
    await waitFor(() => {
      expect(mockInvalidate).toHaveBeenCalledWith({
        queryKey: ["workspaces", "ws-1", "mcp-connectors", "list"],
      });
    });
  });
});
