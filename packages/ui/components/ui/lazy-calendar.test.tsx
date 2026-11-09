import type { ComponentProps } from "react";
import { act, render, screen, waitFor } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import type { Calendar } from "./calendar";
import { LazyCalendar } from "./lazy-calendar";

const imports = vi.hoisted(() => ({
  count: 0,
  ready: Promise.withResolvers<void>(),
  render: vi.fn(),
}));
vi.mock("./calendar", async () => {
  imports.count++;
  await imports.ready.promise;
  return {
    Calendar: (props: ComponentProps<typeof Calendar>) => {
      imports.render(props);
      return <div data-testid="loaded-calendar" />;
    },
  };
});

it("defers loading until mount and forwards calendar props after resolution", async () => {
  const selected = new Date(2026, 10, 5);
  const onSelect = vi.fn();
  const { rerender, container } = render(<div />);
  expect(imports.count).toBe(0);

  rerender(<LazyCalendar mode="single" selected={selected} onSelect={onSelect} />);
  await waitFor(() => expect(imports.count).toBe(1));
  expect(container.querySelector("[aria-busy=true]")).toBeInTheDocument();
  expect(screen.queryByTestId("loaded-calendar")).not.toBeInTheDocument();
  await act(async () => {
    imports.ready.resolve();
    await vi.dynamicImportSettled();
  });
  expect(await screen.findByTestId("loaded-calendar")).toBeInTheDocument();
  expect(container.querySelector("[aria-busy=true]")).not.toBeInTheDocument();
  expect(imports.render).toHaveBeenLastCalledWith({ mode: "single", selected, onSelect });
  expect(imports.count).toBe(1);

  rerender(<div />);
  const range = { from: selected, to: new Date(2026, 10, 6) };
  rerender(<LazyCalendar mode="range" selected={range} autoFocus />);
  expect(imports.render).toHaveBeenLastCalledWith({ mode: "range", selected: range, autoFocus: true });
  expect(imports.count).toBe(1);
});
