/**
 * The main process cannot read `/api/config` itself, so this component is the
 * only thing that can ever turn hang stack capture on. The cases that matter
 * are the ones where the flag is absent or arrives late — both must leave
 * capture off rather than assume.
 */
import { describe, expect, it, vi, beforeEach } from "vitest";
import { render } from "@testing-library/react";

const configState: { featureFlags: Record<string, boolean> } = { featureFlags: {} };

vi.mock("@multica/core/config", () => ({
  useConfigStore: (selector: (s: typeof configState) => unknown) => selector(configState),
  featureFlagEnabled: (
    flags: Record<string, boolean> | undefined,
    key: string,
    defaultValue = false,
  ) => flags?.[key] ?? defaultValue,
}));

import { DiagnosticsControlReporter } from "./diagnostics-control-reporter";
import { HANG_STACK_CAPTURE_FLAG } from "../../../shared/diagnostics-control";

const setDiagnosticsControl = vi.fn();

beforeEach(() => {
  setDiagnosticsControl.mockClear();
  configState.featureFlags = {};
  Object.defineProperty(window, "desktopAPI", {
    configurable: true,
    value: { setDiagnosticsControl },
  });
});

describe("DiagnosticsControlReporter", () => {
  it("publishes off when the backend has never heard of the flag", () => {
    render(<DiagnosticsControlReporter />);

    expect(setDiagnosticsControl).toHaveBeenCalledWith({ stackCaptureEnabled: false });
  });

  it("publishes off when the flag is explicitly disabled", () => {
    configState.featureFlags = { [HANG_STACK_CAPTURE_FLAG]: false };

    render(<DiagnosticsControlReporter />);

    expect(setDiagnosticsControl).toHaveBeenCalledWith({ stackCaptureEnabled: false });
  });

  it("publishes on when the flag is enabled", () => {
    configState.featureFlags = { [HANG_STACK_CAPTURE_FLAG]: true };

    render(<DiagnosticsControlReporter />);

    expect(setDiagnosticsControl).toHaveBeenCalledWith({ stackCaptureEnabled: true });
  });

  // Config is fetched after auth, so the flags are empty on first render. A
  // component that read them once would publish "off" and never correct it.
  it("re-publishes when the flag arrives after mount", () => {
    const { rerender } = render(<DiagnosticsControlReporter />);
    expect(setDiagnosticsControl).toHaveBeenCalledWith({ stackCaptureEnabled: false });

    configState.featureFlags = { [HANG_STACK_CAPTURE_FLAG]: true };
    rerender(<DiagnosticsControlReporter />);

    expect(setDiagnosticsControl).toHaveBeenLastCalledWith({ stackCaptureEnabled: true });
  });

  it("re-publishes when the flag is revoked", () => {
    configState.featureFlags = { [HANG_STACK_CAPTURE_FLAG]: true };
    const { rerender } = render(<DiagnosticsControlReporter />);

    configState.featureFlags = { [HANG_STACK_CAPTURE_FLAG]: false };
    rerender(<DiagnosticsControlReporter />);

    expect(setDiagnosticsControl).toHaveBeenLastCalledWith({ stackCaptureEnabled: false });
  });

  it("does not re-publish an unchanged value", () => {
    const { rerender } = render(<DiagnosticsControlReporter />);
    expect(setDiagnosticsControl).toHaveBeenCalledTimes(1);

    rerender(<DiagnosticsControlReporter />);

    expect(setDiagnosticsControl).toHaveBeenCalledTimes(1);
  });
});
