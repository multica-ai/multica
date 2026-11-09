import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { LazyCalendar } from "./lazy-calendar";

const imports = vi.hoisted(() => ({ count: 0, ready: Promise.withResolvers<void>() }));
vi.mock("./calendar", async (importOriginal) => {
  imports.count++;
  const module = await importOriginal();
  imports.ready.resolve();
  return module;
});

it("loads on mount and preserves day selection and keyboard navigation", async () => {
  const selected = new Date(2026, 10, 5);
  const onSelect = vi.fn();
  const { rerender, container } = render(<div />);
  expect(imports.count).toBe(0);

  rerender(<LazyCalendar mode="single" selected={selected} onSelect={onSelect} />);
  await act(async () => {
    await imports.ready.promise;
    await vi.dynamicImportSettled();
  });
  await screen.findByRole("grid");
  expect(imports.count).toBe(1);
  const initial = container.querySelector<HTMLButtonElement>(
    `button[data-day="${selected.toLocaleDateString()}"]`,
  )!;
  act(() => initial.focus());
  fireEvent.keyDown(initial, { key: "ArrowRight" });
  const target = new Date(2026, 10, 6);
  await waitFor(() => expect(document.activeElement).toHaveAttribute("data-day", target.toLocaleDateString()));
  fireEvent.click(document.activeElement!);
  expect(onSelect.mock.calls[0]?.[0]).toEqual(target);

  rerender(<div />);
  rerender(<LazyCalendar mode="single" selected={selected} onSelect={onSelect} />);
  await screen.findByRole("grid");
  expect(imports.count).toBe(1);

  const onRangeSelect = vi.fn();
  rerender(<div />);
  rerender(<LazyCalendar mode="range" selected={{ from: selected }} onSelect={onRangeSelect} autoFocus />);
  await waitFor(() => expect(document.activeElement).toHaveAttribute("data-day", selected.toLocaleDateString()));
  fireEvent.click(container.querySelector(`button[data-day="${target.toLocaleDateString()}"]`)!);
  expect(onRangeSelect.mock.calls[0]?.[0]).toEqual({ from: selected, to: target });
});
