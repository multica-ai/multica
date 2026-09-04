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

// Numeric validation, dirty comparison, and import collisions live in ../pricing-drafts.test.ts.
it("opens saved prices in browse mode with Close and no save or numeric inputs", () => {
  open();
  expect(screen.getByText("Close", { selector: "button" })).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Save changes" })).toBeNull();
  expect(screen.queryByRole("spinbutton")).toBeNull();
  expect(
    screen.getByRole("region", { name: "workspace-model" }),
  ).toHaveTextContent("0.0028");
});

it("shows a selected public price immediately without a lookup request", () => {
  const initial = snapshot();
  initial.rows["example/model"]!.cacheRead = 0.0000004 * 1_000_000;
  open(initial);
  fireEvent.change(screen.getByLabelText("Model or provider/model"), {
    target: { value: "example/model" },
  });
  const reference = screen.getByRole("region", { name: "example/model" });
  expect(within(reference).getByText("0.0028")).toBeInTheDocument();
  expect(within(reference).getByText("0.0145")).toBeInTheDocument();
  expect(within(reference).getByText("0.4")).toBeInTheDocument();
  expect(within(reference).getByText("USD / 1M tokens")).toBeInTheDocument();
  expect(
    within(reference).getByRole("link", { name: "View source" }),
  ).toHaveAttribute("href", "https://example.com/prices");
  expect(screen.queryByRole("button", { name: "View price" })).toBeNull();
  expect(mocks.get).not.toHaveBeenCalled();
  expect(mocks.save).not.toHaveBeenCalled();
  expect(mocks.refresh).not.toHaveBeenCalled();
});

it("enables saving only after existing override values actually change", () => {
  open();
  fireEvent.click(screen.getByRole("button", { name: "Edit prices" }));
  const save = screen.getByRole("button", { name: "Save changes" });
  expect(save).toBeDisabled();
  fireEvent.change(screen.getByLabelText("Input"), {
    target: { value: "0.002800" },
  });
  expect(save).toBeDisabled();
  fireEvent.change(screen.getByLabelText("Input"), {
    target: { value: "0.004" },
  });
  expect(save).toBeEnabled();
  fireEvent.change(screen.getByLabelText("Input"), {
    target: { value: "0.0028" },
  });
  expect(save).toBeDisabled();
  expect(mocks.save).not.toHaveBeenCalled();
});

it("customizes a public model without a duplicate reference card or rounding its edit values", async () => {
  const initial = snapshot();
  initial.overrides = {};
  const preciseRate = Number("0.12345678901234567");
  initial.rows["example/model"]!.input = preciseRate;
  open(initial);
  fireEvent.change(screen.getByLabelText("Model or provider/model"), {
    target: { value: "example/model" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Customize" }));
  expect(screen.queryByRole("region", { name: "example/model" })).toBeNull();
  expect(screen.getByLabelText("Input")).toHaveValue(preciseRate);
  expect(screen.getByLabelText("Output")).toHaveValue(0.0145);
  expect(screen.getByRole("button", { name: "Save changes" })).toBeEnabled();
  fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
  await waitFor(() =>
    expect(mocks.save).toHaveBeenCalledWith("workspace-a", {
      revision: 3,
      overrides: { "example/model": { ...rate, input: preciseRate } },
    }),
  );
});

it("lets unknown models enter custom pricing without prematurely enabling Save", () => {
  open({ ...snapshot(), overrides: {} });
  fireEvent.change(screen.getByLabelText("Model or provider/model"), {
    target: { value: "private-model" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Customize" }));
  expect(screen.getByRole("button", { name: "Save changes" })).toBeDisabled();
  for (const label of ["Input", "Output", "Cache read", "Cache write"]) {
    fireEvent.change(screen.getByLabelText(label), { target: { value: "0" } });
  }
  expect(screen.getByRole("button", { name: "Save changes" })).toBeEnabled();
});

it("stages removal and returns to browsing when editing is cancelled", () => {
  const { close } = open();
  fireEvent.click(screen.getByRole("button", { name: "Edit prices" }));
  fireEvent.click(screen.getByRole("button", { name: "Remove custom price" }));
  expect(screen.getByRole("button", { name: "Save changes" })).toBeEnabled();
  expect(mocks.save).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
  expect(screen.queryByRole("button", { name: "Save changes" })).toBeNull();
  expect(
    screen.getByRole("region", { name: "workspace-model" }),
  ).toBeInTheDocument();
  expect(close).not.toHaveBeenCalled();
});

it("keeps an edited draft through refetch and revision conflict until explicit reload", async () => {
  mocks.save.mockRejectedValue(new ApiError("conflict", 409, "Conflict"));
  const { client, close } = open();
  fireEvent.click(screen.getByRole("button", { name: "Edit prices" }));
  fireEvent.change(screen.getByLabelText("Input"), { target: { value: "7" } });
  const latest = {
    ...snapshot(),
    revision: 4,
    overrides: { "workspace-model": { ...rate, input: 5 } },
  };
  mocks.get.mockResolvedValue(latest);
  client.setQueryData(modelPricingKey("workspace-a"), latest);
  await waitFor(() => expect(screen.getByLabelText("Input")).toHaveValue(7));
  fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
  await screen.findByText(/Another admin updated/);
  expect(mocks.save).toHaveBeenCalledWith("workspace-a", {
    revision: 3,
    overrides: { "workspace-model": { ...rate, input: 7 } },
  });
  expect(screen.getByLabelText("Input")).toHaveValue(7);
  expect(close).not.toHaveBeenCalled();
  expect(screen.getByRole("button", { name: "Save changes" })).toBeDisabled();
  fireEvent.click(
    screen.getByRole("button", { name: "Discard drafts and reload" }),
  );
  await waitFor(() => expect(screen.getByLabelText("Input")).toHaveValue(5));
  expect(screen.getByRole("button", { name: "Save changes" })).toBeDisabled();
});

it("shows automatic read updates to members without edit or refresh actions", async () => {
  mocks.role = "member";
  const { client } = open();
  expect(screen.queryByRole("spinbutton")).toBeNull();
  expect(screen.queryByRole("button", { name: "Edit prices" })).toBeNull();
  expect(screen.queryByRole("button", { name: "Check now" })).toBeNull();
  client.setQueryData(modelPricingKey("workspace-a"), {
    ...snapshot(),
    revision: 4,
    overrides: { "workspace-model": { ...rate, input: 3 } },
  });
  await waitFor(() =>
    expect(
      within(screen.getByRole("region", { name: "workspace-model" })).getByText(
        "3",
      ),
    ).toBeInTheDocument(),
  );
});

it("captures the latest revision when browsing becomes editing", async () => {
  const { client } = open();
  client.setQueryData(modelPricingKey("workspace-a"), {
    ...snapshot(),
    revision: 4,
    overrides: { "workspace-model": { ...rate, input: 3 } },
  });
  await waitFor(() =>
    expect(
      within(screen.getByRole("region", { name: "workspace-model" })).getByText(
        "3",
      ),
    ).toBeInTheDocument(),
  );
  fireEvent.click(screen.getByRole("button", { name: "Edit prices" }));
  fireEvent.change(screen.getByLabelText("Input"), { target: { value: "5" } });
  fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
  await waitFor(() =>
    expect(mocks.save).toHaveBeenCalledWith("workspace-a", {
      revision: 4,
      overrides: { "workspace-model": { ...rate, input: 5 } },
    }),
  );
});

it("requires both current membership and server permission to start editing", () => {
  open({ ...snapshot(), canManage: false });
  expect(screen.queryByRole("button", { name: "Edit prices" })).toBeNull();
  fireEvent.change(screen.getByLabelText("Model or provider/model"), {
    target: { value: "example/model" },
  });
  expect(screen.queryByRole("button", { name: "Customize" })).toBeNull();
  expect(screen.queryByRole("spinbutton")).toBeNull();
});

it("keeps synchronization controls and full timestamps in collapsed pricing details", () => {
  open({ ...snapshot(), succeededAt: "2026-09-03T01:02:03Z" });
  const details = screen.getByText("Pricing details").closest("details")!;
  expect(details).not.toHaveAttribute("open");
  expect(screen.getByText("Updated Sep 3")).toBeInTheDocument();
  expect(within(details).getByText(/Checked daily at 00:00/)).not.toBeVisible();
  const refresh = within(details).getByRole("button", {
    name: "Check now",
    hidden: true,
  });
  expect(refresh).not.toBeVisible();
  fireEvent.click(details.querySelector("summary")!);
  expect(refresh).toBeVisible();
});

it.each(["Local", "Malformed/Timezone"])(
  "keeps date rendering usable when the server timezone is %s",
  (timezone) => {
    open({ ...snapshot(), succeededAt: "2026-09-03T01:02:03Z", timezone });
    expect(screen.getByText("Updated Sep 3")).toBeInTheDocument();
    const details = screen.getByText("Pricing details").closest("details")!;
    fireEvent.click(details.querySelector("summary")!);
    expect(within(details).getByText(/Last successful update:/)).toBeVisible();
  },
);

it("previews local imports without a request and cancels back to browsing", () => {
  const stored = JSON.stringify({
    state: { pricings: { "local-model": rate } },
    version: 0,
  });
  localStorage.setItem(legacyKey, stored);
  const { close } = open();
  fireEvent.click(screen.getByRole("button", { name: "Preview local prices" }));
  expect(
    screen.getByText("Local prices added: 1. Save to share them."),
  ).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Save changes" })).toBeEnabled();
  expect(mocks.save).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
  expect(screen.queryByRole("button", { name: "Save changes" })).toBeNull();
  expect(close).not.toHaveBeenCalled();
  expect(localStorage.getItem(legacyKey)).toBe(stored);
});

it("clears only successfully imported prices after explicit save", async () => {
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
  fireEvent.click(screen.getByRole("button", { name: "Preview local prices" }));
  fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
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
  expect(screen.queryByRole("button", { name: "Save changes" })).toBeNull();
});
