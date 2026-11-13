import * as React from "react";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { TimeInput, type TimeInputProps } from "./time-input";

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

function mount(initial = "09:30", props: Partial<TimeInputProps> = {}) {
  const onChange = vi.fn();
  const onKeyDown = vi.fn();
  function Harness() {
    const [value, setValue] = React.useState(initial);
    return (
      <div onKeyDown={onKeyDown}>
        <TimeInput {...props} value={value} onChange={(next) => {
          onChange(next);
          setValue(next);
        }} />
        <output data-testid="value">{value}</output>
        <button>Elsewhere</button>
      </div>
    );
  }
  render(<Harness />);
  return {
    hour: screen.getByLabelText(props.hourLabel ?? "Hour"),
    minute: () => screen.getByLabelText(props.minuteLabel ?? "Minute"),
    value: () => screen.getByTestId("value").textContent,
    onChange,
    onKeyDown,
  };
}

describe("TimeInput keyboard editing", () => {
  for (const modifier of ["ctrlKey", "metaKey", "altKey"] as const) {
    for (const segment of ["hour", "minute"] as const) {
      it(`leaves ${modifier} shortcuts on the ${segment} to the browser or owner`, () => {
        const h = mount();
        const input = segment === "hour" ? h.hour : h.minute();
        act(() => { input.focus(); });
        for (const key of ["1", "ArrowUp", "ArrowDown", "ArrowLeft", "ArrowRight", "Backspace", "Delete", "Tab"]) {
          expect(fireEvent.keyDown(input, { key, [modifier]: true })).toBe(true);
          expect(input).toHaveFocus();
          expect(h.value()).toBe("09:30");
          expect(h.onChange).not.toHaveBeenCalled();
          expect(h.onKeyDown).toHaveBeenLastCalledWith(expect.objectContaining({ key, defaultPrevented: false }));
        }
      });
    }

    it(`does not start a digit pair for ${modifier}+digit`, () => {
      const h = mount();
      act(() => { h.hour.focus(); });
      fireEvent.keyDown(h.hour, { key: "1", [modifier]: true });
      fireEvent.keyDown(h.hour, { key: "5" });
      expect(h.value()).toBe("05:30");
      expect(h.hour).toHaveFocus();
    });

    it(`preserves a pending digit and its timeout across ${modifier} shortcuts`, () => {
      vi.useFakeTimers();
      const h = mount("09:30", { hourMin: 9 });
      act(() => { h.hour.focus(); });
      fireEvent.keyDown(h.hour, { key: "1" });
      expect(h.value()).toBe("09:30");
      act(() => { vi.advanceTimersByTime(1000); });
      for (const key of ["7", "ArrowUp", "ArrowDown", "ArrowLeft", "ArrowRight", "Backspace", "Delete"]) {
        expect(fireEvent.keyDown(h.hour, { key, [modifier]: true })).toBe(true);
      }
      fireEvent.keyDown(h.hour, { key: "5" });
      expect(h.value()).toBe("15:30");
      expect(h.minute()).toHaveFocus();

      act(() => { h.hour.focus(); });
      fireEvent.keyDown(h.hour, { key: "1" });
      act(() => { vi.advanceTimersByTime(1000); });
      fireEvent.keyDown(h.hour, { key: "8", [modifier]: true });
      act(() => { vi.advanceTimersByTime(1001); });
      fireEvent.keyDown(h.hour, { key: "5" });
      expect(h.value()).toBe("09:30");
      expect(h.hour).toHaveFocus();
    });
  }

  it("leaves Ctrl+Alt digit events (including AltGraph flags) unhandled", () => {
    const h = mount();
    expect(fireEvent.keyDown(h.hour, { key: "1", ctrlKey: true, altKey: true })).toBe(true);
    expect(h.onChange).not.toHaveBeenCalled();
    expect(h.value()).toBe("09:30");
  });

  it("keeps ordinary digit pairs, segment navigation and Shift behavior", () => {
    const h = mount();
    act(() => { h.hour.focus(); });
    fireEvent.keyDown(h.hour, { key: "1" });
    fireEvent.keyDown(h.hour, { key: "7" });
    expect(h.minute()).toHaveFocus();
    fireEvent.keyDown(h.minute(), { key: "5" });
    fireEvent.keyDown(h.minute(), { key: "8" });
    expect(h.value()).toBe("17:58");
    fireEvent.keyDown(h.minute(), { key: "ArrowLeft" });
    expect(h.hour).toHaveFocus();
    fireEvent.keyDown(h.hour, { key: "ArrowRight" });
    expect(h.minute()).toHaveFocus();
    expect(fireEvent.keyDown(h.minute(), { key: "Tab", shiftKey: true })).toBe(true);
    fireEvent.keyDown(h.minute(), { key: "ArrowUp", shiftKey: true });
    expect(h.value()).toBe("17:59");
    fireEvent.keyDown(h.minute(), { key: "Delete" });
    expect(h.value()).toBe("17:00");
  });

  it("wraps hours and minutes independently", () => {
    const h = mount("23:59");
    fireEvent.keyDown(h.hour, { key: "ArrowUp" });
    fireEvent.keyDown(h.minute(), { key: "ArrowUp" });
    expect(h.value()).toBe("00:00");
    fireEvent.keyDown(h.hour, { key: "ArrowDown" });
    fireEvent.keyDown(h.minute(), { key: "ArrowDown" });
    expect(h.value()).toBe("23:59");
  });

  it("keeps bounded end hours and their clear target", () => {
    const h = mount("09:30", { hourMin: 9, hourClearTo: 23 });
    fireEvent.keyDown(h.hour, { key: "ArrowDown" });
    expect(h.value()).toBe("23:30");
    fireEvent.keyDown(h.hour, { key: "0" });
    fireEvent.keyDown(h.hour, { key: "8" });
    expect(h.value()).toBe("09:30");
    fireEvent.keyDown(h.hour, { key: "Backspace" });
    expect(h.value()).toBe("23:30");
  });

  it("ends a digit pair on blur", () => {
    const h = mount();
    act(() => { h.hour.focus(); });
    fireEvent.keyDown(h.hour, { key: "1" });
    act(() => { screen.getByRole("button").focus(); });
    act(() => { h.hour.focus(); });
    fireEvent.keyDown(h.hour, { key: "5" });
    expect(h.value()).toBe("05:30");
  });

  it("preserves disabled, hour-only and accessible labels", () => {
    const h = mount("09:30", { disabled: true, hourOnly: true, hourLabel: "Window end hour" });
    expect(h.hour).toBeDisabled();
    expect(h.hour).toHaveAccessibleName("Window end hour");
    expect(screen.queryByLabelText("Minute")).not.toBeInTheDocument();
    h.hour.focus();
    expect(h.hour).not.toHaveFocus();
    expect(h.onChange).not.toHaveBeenCalled();
  });
});
