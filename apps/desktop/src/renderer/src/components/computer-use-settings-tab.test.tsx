import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import translations from "../../../../../../packages/views/locales/en/settings.json";

const mocks = vi.hoisted(() => ({ list: vi.fn(), remove: vi.fn(), clear: vi.fn() }));
vi.mock("@multica/views/i18n", () => ({ useT: () => ({ t: (selector: (resources: typeof translations) => string, values?: Record<string, string>) => Object.entries(values ?? {}).reduce((text, [key, value]) => text.replace(`{{${key}}}`, value), selector(translations)) }) }));
import { ComputerUseSettingsTab } from "./computer-use-settings-tab";

const copy = translations.desktop.computer_use;
const calculator = { id: "com.apple.calculator", name: "Calculator" };
const textEdit = { id: "com.apple.TextEdit", name: "TextEdit" };
const clients: QueryClient[] = [];
function renderTab() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  clients.push(client);
  return render(<QueryClientProvider client={client}><ComputerUseSettingsTab /></QueryClientProvider>);
}
beforeEach(() => {
  vi.resetAllMocks();
  Object.defineProperty(window, "desktopAPI", { configurable: true, value: { computerUseApprovals: mocks } });
});
afterEach(() => { cleanup(); clients.splice(0).forEach((client) => client.clear()); });

it("lists apps that don't ask again and removes one", async () => {
  mocks.list.mockResolvedValue({ available: true, apps: [calculator, textEdit] });
  mocks.remove.mockResolvedValue({ available: true, apps: [textEdit] });
  renderTab();
  fireEvent.click(await screen.findByRole("button", { name: "Remove Calculator" }));
  await waitFor(() => expect(screen.queryByText("Calculator")).not.toBeInTheDocument());
  expect(mocks.remove).toHaveBeenCalledExactlyOnceWith("com.apple.calculator");
  expect(screen.getByText("TextEdit")).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: copy.clear })).not.toBeInTheDocument();
});

it("removes all apps and then shows the empty state", async () => {
  mocks.list.mockResolvedValue({ available: true, apps: [calculator, textEdit] });
  mocks.clear.mockResolvedValue({ available: true, apps: [] });
  renderTab();
  fireEvent.click(await screen.findByRole("button", { name: copy.clear }));
  expect(await screen.findByText(copy.empty)).toBeInTheDocument();
  expect(mocks.clear).toHaveBeenCalledOnce();
});

it("keeps the list and reports a failed change", async () => {
  mocks.list.mockResolvedValue({ available: true, apps: [calculator] });
  mocks.remove.mockRejectedValue(new Error("private failure"));
  renderTab();
  fireEvent.click(await screen.findByRole("button", { name: "Remove Calculator" }));
  expect(await screen.findByRole("alert")).toHaveTextContent(copy.failed);
  expect(screen.getByText("Calculator")).toBeInTheDocument();
  expect(screen.queryByText("private failure")).not.toBeInTheDocument();
});
