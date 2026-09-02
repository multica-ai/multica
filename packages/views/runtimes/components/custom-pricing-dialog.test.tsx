import {
  cleanup,
  fireEvent,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { ApiError } from "@multica/core/api";
import { type ModelPricingSnapshot } from "@multica/core/runtimes/pricing";
import { modelPricingKey } from "@multica/core/runtimes/pricing-queries";
import { renderWithI18n } from "../../test/i18n";
import { CustomPricingDialog } from "./custom-pricing-dialog";

const mocks = vi.hoisted(() => ({
  role: "owner",
  get: vi.fn(),
  save: vi.fn(),
  refresh: vi.fn(),
}));
vi.mock("@multica/core/permissions", () => ({
  useCurrentMember: () => ({ role: mocks.role }),
}));
vi.mock("@multica/core/api", async (original) => {
  const actual = await original<typeof import("@multica/core/api")>();
  return {
    ...actual,
    api: {
      ...actual.api,
      getModelPricing: mocks.get,
      saveModelPricing: mocks.save,
      refreshModelPricing: mocks.refresh,
    },
  };
});

const rate = {
  input: 0.0028,
  output: 0.0145,
  cacheRead: 0.0000001,
  cacheWrite: 0,
};
const legacyKey = "multica_runtime_custom_pricing";
function snapshot(): ModelPricingSnapshot {
  return {
    rows: {
      "example/model": {
        ...rate,
        source: "Example",
        sourceUrl: "https://example.com/prices",
      },
    },
    aliases: {},
    version: "test",
    revision: 3,
    overrides: { "workspace-model": rate },
    canManage: true,
    checkedAt: null,
    succeededAt: null,
    lastError: "",
    timezone: "UTC",
  };
}

function open(initial = snapshot()) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  client.setQueryData(modelPricingKey("workspace-a"), initial);
  const close = vi.fn();
  renderWithI18n(
    <QueryClientProvider client={client}>
      <CustomPricingDialog
        wsId="workspace-a"
        open
        onOpenChange={close}
        unmappedModels={[]}
      />
    </QueryClientProvider>,
  );
  return { client, close };
}

beforeEach(() => {
  mocks.role = "owner";
  mocks.get.mockResolvedValue(snapshot());
  mocks.save.mockResolvedValue({ ...snapshot(), revision: 4 });
  mocks.refresh.mockResolvedValue(snapshot());
  localStorage.clear();
});
afterEach(() => {
  cleanup();
  vi.resetAllMocks();
});

// Numeric validation and import collision rules live in ../pricing-drafts.test.ts.
it("saves the captured workspace revision without rounding small prices", async () => {
  const { close } = open();
  expect(screen.getByLabelText("Input")).toHaveValue(0.0028);
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  await waitFor(() =>
    expect(mocks.save).toHaveBeenCalledWith("workspace-a", {
      revision: 3,
      overrides: { "workspace-model": rate },
    }),
  );
  await waitFor(() => expect(close).toHaveBeenCalledWith(false));
});

it("stages removal until the user saves", async () => {
  open();
  fireEvent.click(screen.getByRole("button", { name: "Remove custom price" }));
  expect(mocks.save).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  await waitFor(() =>
    expect(mocks.save).toHaveBeenCalledWith("workspace-a", {
      revision: 3,
      overrides: {},
    }),
  );
});

it("keeps a conflicting draft until the user explicitly reloads the latest revision", async () => {
  mocks.save.mockRejectedValue(new ApiError("conflict", 409, "Conflict"));
  mocks.get.mockResolvedValue({ ...snapshot(), revision: 4 });
  const { close } = open();
  fireEvent.change(screen.getByLabelText("Input"), { target: { value: "7" } });
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  await screen.findByText(/Another administrator updated/);
  expect(screen.getByLabelText("Input")).toHaveValue(7);
  expect(close).not.toHaveBeenCalled();
  expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
  fireEvent.click(
    screen.getByRole("button", { name: "Discard drafts and reload" }),
  );
  await waitFor(() =>
    expect(screen.getByLabelText("Input")).toHaveValue(rate.input),
  );
  mocks.save.mockResolvedValue({ ...snapshot(), revision: 5 });
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  await waitFor(() =>
    expect(mocks.save).toHaveBeenLastCalledWith("workspace-a", {
      revision: 4,
      overrides: { "workspace-model": rate },
    }),
  );
});

it("allows members to inspect current prices without write actions", async () => {
  mocks.role = "member";
  const { client } = open();
  expect(screen.getByLabelText("Input")).toBeDisabled();
  expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
  expect(screen.queryByRole("button", { name: "Check now" })).toBeNull();
  client.setQueryData(modelPricingKey("workspace-a"), {
    ...snapshot(),
    revision: 4,
    overrides: { "workspace-model": { ...rate, input: 3 } },
  });
  await waitFor(() => expect(screen.getByLabelText("Input")).toHaveValue(3));
});

it("requires both current membership and server permission for writes", () => {
  open({ ...snapshot(), canManage: false });
  expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
  expect(screen.getByLabelText("Input")).toBeDisabled();
});

it("shows precise public prices and their source without creating an override", async () => {
  const initial = snapshot();
  initial.rows["example/model"]!.cacheRead = 0.0000004 * 1_000_000;
  open(initial);
  fireEvent.change(screen.getByLabelText("Model or provider/model"), {
    target: { value: "example/model" },
  });
  fireEvent.click(screen.getByRole("button", { name: "View price" }));
  const reference = screen.getByRole("region", {
    name: "Public API reference",
  });
  expect(within(reference).getByText("0.0028")).toBeInTheDocument();
  expect(within(reference).getByText("0.0145")).toBeInTheDocument();
  expect(within(reference).getByText("0.4")).toBeInTheDocument();
  expect(
    within(reference).getByRole("link", { name: "View source" }),
  ).toHaveAttribute("href", "https://example.com/prices");
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  await waitFor(() =>
    expect(mocks.save).toHaveBeenCalledWith("workspace-a", {
      revision: 3,
      overrides: { "workspace-model": rate },
    }),
  );
});

it("previews local prices without uploading or removing them on cancel", () => {
  const stored = JSON.stringify({
    state: { pricings: { "local-model": rate } },
    version: 0,
  });
  localStorage.setItem(legacyKey, stored);
  const { close } = open();
  fireEvent.click(
    screen.getByRole("button", { name: "Preview local prices for import" }),
  );
  expect(
    screen.getByText(/Local prices added to this preview: 1/),
  ).toBeInTheDocument();
  expect(screen.getByText("local-model")).toBeInTheDocument();
  expect(mocks.save).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
  expect(close).toHaveBeenCalledWith(false);
  expect(localStorage.getItem(legacyKey)).toBe(stored);
});

it("clears only successfully imported local prices after explicit save", async () => {
  localStorage.setItem(
    legacyKey,
    JSON.stringify({
      state: {
        pricings: {
          "local-model": rate,
          "workspace-model": { ...rate, input: 99 },
        },
      },
      version: 0,
    }),
  );
  open();
  fireEvent.click(
    screen.getByRole("button", { name: "Preview local prices for import" }),
  );
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  await waitFor(() =>
    expect(mocks.save).toHaveBeenCalledWith("workspace-a", {
      revision: 3,
      overrides: { "workspace-model": rate, "local-model": rate },
    }),
  );
  await waitFor(() =>
    expect(JSON.parse(localStorage.getItem(legacyKey)!)).toEqual({
      state: { pricings: { "workspace-model": { ...rate, input: 99 } } },
      version: 0,
    }),
  );
});
