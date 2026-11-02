// @vitest-environment jsdom

// OL-466: PropertyPicker routes non-<button> host-element triggerRenders down
// Base UI's non-native trigger path (nativeButton={false}). Base UI dev-checks
// both directions — a non-<button> rendered while nativeButton stays true
// errors, and so does a <button> rendered while it is false. These tests pin
// the branch both ways under the real Popover, catching console.error from
// either side.
//
// The render-element's own role wins Base UI's injected one (mergeProps:
// external props take precedence), which is what lets label-picker's chip-wrap
// div declare role="combobox" instead of receiving role="button" over nested
// remove buttons.
import { afterEach, describe, expect, it, vi } from "vitest";
import { forwardRef } from "react";
import { cleanup } from "@testing-library/react";
import { renderWithI18n } from "../../../test/i18n";
import { PillButton } from "../../../common/pill-button";
import { PropertyPicker } from "./property-picker";

afterEach(cleanup);

function expectNoBaseUiErrors(renderUi: () => void) {
  const consoleError = vi.spyOn(console, "error").mockImplementation(() => {});
  try {
    renderUi();
    const baseUiErrors = consoleError.mock.calls.filter(([message]) =>
      String(message).startsWith("Base UI:"),
    );
    expect(baseUiErrors).toEqual([]);
  } finally {
    consoleError.mockRestore();
  }
}

describe("PropertyPicker trigger nativeButton routing", () => {
  it("honors explicit non-native semantics for a component trigger", () => {
    const NonNativeTrigger = forwardRef<HTMLDivElement, React.ComponentProps<"div">>(
      (props, ref) => <div {...props} ref={ref} />,
    );
    NonNativeTrigger.displayName = "NonNativeTrigger";
    expectNoBaseUiErrors(() =>
      renderWithI18n(
        <PropertyPicker open onOpenChange={() => {}} triggerRender={<NonNativeTrigger />} nativeButton={false} trigger="Choose">
          <button type="button" data-picker-item>row</button>
        </PropertyPicker>,
      ),
    );
  });

  it("renders a non-button host-element anchor without Base UI errors", () => {
    expectNoBaseUiErrors(() =>
      renderWithI18n(
        <PropertyPicker
          open
          onOpenChange={() => {}}
          triggerRender={
            <span aria-hidden className="pointer-events-none fixed" />
          }
          trigger={<span />}
        >
          <button type="button" data-picker-item>
            row
          </button>
        </PropertyPicker>,
      ),
    );
  });

  it("renders a host <button> trigger without Base UI errors", () => {
    expectNoBaseUiErrors(() =>
      renderWithI18n(
        <PropertyPicker
          open
          onOpenChange={() => {}}
          triggerRender={<button type="button" className="chip" />}
          trigger="Assign"
        >
          <button type="button" data-picker-item>
            row
          </button>
        </PropertyPicker>,
      ),
    );
  });

  it("renders a component trigger (PillButton) without Base UI errors", () => {
    expectNoBaseUiErrors(() =>
      renderWithI18n(
        <PropertyPicker
          open
          onOpenChange={() => {}}
          triggerRender={<PillButton />}
          trigger="Unassigned"
        >
          <button type="button" data-picker-item>
            row
          </button>
        </PropertyPicker>,
      ),
    );
  });

  it("keeps an explicit role on a non-button host element over Base UI's injected one", () => {
    const { getByRole } = renderWithI18n(
      <PropertyPicker
        open
        onOpenChange={() => {}}
        triggerRender={<div role="combobox" className="chip-wrap" />}
        trigger="labels"
      >
        <button type="button" data-picker-item>
          row
        </button>
      </PropertyPicker>,
    );
    expect(getByRole("combobox")).toBeInTheDocument();
  });
});
