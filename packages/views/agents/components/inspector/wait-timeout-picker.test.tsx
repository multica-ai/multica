// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../../locales/en/common.json";
import enAgents from "../../../locales/en/agents.json";
import enIssues from "../../../locales/en/issues.json";

import { WaitTimeoutPicker } from "./wait-timeout-picker";

const TEST_RESOURCES = {
  en: { common: enCommon, agents: enAgents, issues: enIssues },
};

function renderPicker(
  props: Partial<React.ComponentProps<typeof WaitTimeoutPicker>> = {},
) {
  const onChange = vi.fn().mockResolvedValue(undefined);
  const utils = render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <WaitTimeoutPicker
        valueSeconds={null}
        canEdit
        onChange={onChange}
        {...props}
      />
    </I18nProvider>,
  );
  return { ...utils, onChange };
}

describe("WaitTimeoutPicker", () => {
  beforeEach(() => {
    cleanup();
  });

  afterEach(() => {
    cleanup();
  });

  it('renders "Global" when queued_ttl_seconds is absent and canEdit=false', () => {
    renderPicker({ valueSeconds: null, canEdit: false });

    expect(screen.getByText("Global")).toBeInTheDocument();
    expect(screen.queryByRole("button")).toBeNull();
  });

  it("converts minutes to seconds when saving a positive integer", async () => {
    const { onChange } = renderPicker({ valueSeconds: 1800 });

    fireEvent.click(screen.getByRole("button"));
    fireEvent.change(screen.getByRole("spinbutton"), {
      target: { value: "45" },
    });
    fireEvent.click(screen.getByText("Save"));

    await waitFor(() => {
      expect(onChange).toHaveBeenCalledWith(2700);
    });
  });

  it("sends 0 when the input is cleared before saving", async () => {
    const { onChange } = renderPicker({ valueSeconds: 1800 });

    fireEvent.click(screen.getByRole("button"));
    fireEvent.change(screen.getByRole("spinbutton"), {
      target: { value: "" },
    });
    fireEvent.click(screen.getByText("Save"));

    await waitFor(() => {
      expect(onChange).toHaveBeenCalledWith(0);
    });
  });

  it("does not fire onChange when the current value already follows the global default and the draft stays blank", async () => {
    const { onChange } = renderPicker({ valueSeconds: null });

    fireEvent.click(screen.getByRole("button"));
    fireEvent.click(screen.getByText("Save"));

    await waitFor(() => {
      expect(onChange).not.toHaveBeenCalled();
    });
  });

  it("does not fire onChange when the current value already follows the global default and the user enters 0", async () => {
    const { onChange } = renderPicker({ valueSeconds: null });

    fireEvent.click(screen.getByRole("button"));
    fireEvent.change(screen.getByRole("spinbutton"), {
      target: { value: "0" },
    });
    fireEvent.click(screen.getByText("Save"));

    await waitFor(() => {
      expect(onChange).not.toHaveBeenCalled();
    });
  });

  it("does not fire onChange when a non-minute-aligned persisted value is opened and saved without editing", async () => {
    const { onChange } = renderPicker({ valueSeconds: 3599 });

    fireEvent.click(screen.getByRole("button"));
    fireEvent.click(screen.getByText("Save"));

    await waitFor(() => {
      expect(onChange).not.toHaveBeenCalled();
    });
  });

  it("focuses the input when the visible label is clicked", async () => {
    const user = userEvent.setup();
    renderPicker({ valueSeconds: 1800 });

    fireEvent.click(screen.getByRole("button"));
    const input = screen.getByRole("spinbutton");
    await user.click(screen.getByText("Queue wait timeout (minutes)"));

    expect(input).toHaveFocus();
  });
});
