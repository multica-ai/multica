// @vitest-environment jsdom

import { cleanup, fireEvent, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { renderWithI18n } from "../../test/i18n";

const storeState = vi.hoisted(() => ({
  pricings: {} as Record<
    string,
    {
      input: number;
      output: number;
      cacheRead: number;
      cacheWrite: number;
    }
  >,
  setCustomPricing: vi.fn(),
  removeCustomPricing: vi.fn(),
}));

vi.mock("@multica/core/runtimes/custom-pricing-store", () => {
  const useCustomPricingStore = Object.assign(
    (sel?: (s: typeof storeState) => unknown) => (sel ? sel(storeState) : storeState),
    { getState: () => storeState },
  );
  return { useCustomPricingStore };
});

import { ModelPricingTab } from "./model-pricing-tab";

describe("ModelPricingTab", () => {
  beforeEach(() => {
    storeState.pricings = {};
    storeState.setCustomPricing.mockClear();
    storeState.removeCustomPricing.mockClear();
    cleanup();
  });

  it("defaults a blank cache-write rate to the output price when saving", () => {
    renderWithI18n(<ModelPricingTab />);

    fireEvent.click(screen.getByRole("button", { name: "Add model" }));
    fireEvent.change(screen.getByPlaceholderText("Model name (e.g. mimo-v2.5-pro)"), {
      target: { value: "mimo-v2.5-pro" },
    });
    const rateInputs = screen.getAllByRole("spinbutton");
    fireEvent.change(rateInputs[0]!, { target: { value: "1" } });
    fireEvent.change(rateInputs[1]!, { target: { value: "2" } });
    fireEvent.change(rateInputs[2]!, { target: { value: "0.5" } });

    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    expect(storeState.setCustomPricing).toHaveBeenCalledWith("mimo-v2.5-pro", {
      input: 1,
      output: 2,
      cacheRead: 0.5,
      cacheWrite: 2,
    });
  });

  it("allows built-in models to be added as custom pricing overrides", () => {
    renderWithI18n(<ModelPricingTab />);

    fireEvent.click(screen.getByRole("button", { name: "Add model" }));
    fireEvent.change(screen.getByPlaceholderText("Model name (e.g. mimo-v2.5-pro)"), {
      target: { value: "claude-sonnet-4-6" },
    });

    const rateInputs = screen.getAllByRole("spinbutton");
    for (const input of rateInputs) {
      fireEvent.change(input, { target: { value: "1" } });
    }

    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    expect(storeState.setCustomPricing).toHaveBeenCalledWith("claude-sonnet-4-6", {
      input: 1,
      output: 1,
      cacheRead: 1,
      cacheWrite: 1,
    });
  });

  it("keeps built-in custom overrides editable in the list", () => {
    storeState.pricings = {
      "claude-sonnet-4-6": {
        input: 1,
        output: 2,
        cacheRead: 0.5,
        cacheWrite: 1,
      },
    };

    renderWithI18n(<ModelPricingTab />);

    expect(screen.getByLabelText("Edit custom pricing")).toBeInTheDocument();
  });
});
