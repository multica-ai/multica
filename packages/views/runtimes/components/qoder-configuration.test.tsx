import { vi, it, expect, beforeEach, afterEach } from "vitest";
import {
  render,
  screen,
  fireEvent,
  waitFor,
  cleanup,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import enRuntimes from "../../locales/en/runtimes.json";
import { QoderConfiguration } from "./qoder-configuration";

const mocks = vi.hoisted(() => ({
  getQoderConnection: vi.fn(),
  listMembers: vi.fn(),
  listQoderEnvironments: vi.fn(),
  checkQoderConnection: vi.fn(),
}));
vi.mock("@multica/core/api", () => ({ api: mocks }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-test" }));
vi.mock("@multica/core/auth", () => {
  const state = { user: { id: "owner" } };
  return {
    useAuthStore: Object.assign(
      (selector: (s: typeof state) => unknown) => selector(state),
      { getState: () => state },
    ),
  };
});
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

beforeEach(() => {
  mocks.getQoderConnection.mockResolvedValue({
    available: true,
    configured: true,
    name: "QCA",
    baseUrl: "https://api.qoder.com/api/v1/cloud",
    environmentId: "",
    hasToken: true,
    repositories: [],
    enabled: true,
    status: "online",
    lastError: "",
  });
  mocks.listMembers.mockResolvedValue([{ user_id: "owner", role: "owner" }]);
  mocks.checkQoderConnection.mockResolvedValue(undefined);
});
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});
function mount() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  render(
    <I18nProvider locale="en" resources={{ en: { runtimes: enRuntimes } }}>
      <QueryClientProvider client={client}>
        <QoderConfiguration />
      </QueryClientProvider>
    </I18nProvider>,
  );
  return client;
}
it("selects the only environment, hides saved PATs and reports connection success", async () => {
  mocks.listQoderEnvironments.mockResolvedValue([
    { id: "env_one", name: "Web environment" },
  ]);
  const client = mount();
  const check = await screen.findByRole("button", { name: "Test connection" });
  await waitFor(() => expect(check).toBeEnabled());
  expect(
    screen.getByRole("combobox", { name: "Environment" }),
  ).toHaveTextContent("Web environment");
  expect(screen.queryByText("QCA Agent ID")).not.toBeInTheDocument();
  expect(screen.getByPlaceholderText("Saved, never displayed")).toBeDisabled();
  fireEvent.click(check);
  await screen.findByText("Connection successful");
  expect(mocks.checkQoderConnection).toHaveBeenCalledWith(
    "ws-test",
    expect.objectContaining({ environmentId: "env_one", qoderToken: "" }),
  );
  client.clear();
});
it("requires an explicit choice when several environments are available", async () => {
  mocks.listQoderEnvironments.mockResolvedValue([
    { id: "env_one", name: "Web" },
    { id: "env_two", name: "Build" },
  ]);
  const client = mount();
  await waitFor(() =>
    expect(screen.getByRole("combobox", { name: "Environment" })).toBeEnabled(),
  );
  expect(
    screen.getByRole("combobox", { name: "Environment" }),
  ).toHaveTextContent("Select an environment");
  expect(
    screen.getByRole("button", { name: "Test connection" }),
  ).toBeDisabled();
  client.clear();
});
