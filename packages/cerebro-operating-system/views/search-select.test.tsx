import "@testing-library/jest-dom/vitest";

import { fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
import { describe, expect, it, vi } from "vitest";

import { SearchSelect, type SearchSelectOption } from "./search-select";

const options: SearchSelectOption[] = [
  { value: "period-1", label: "Q3 2026", hint: "Jul 1 – Sep 30" },
  { value: "period-2", label: "Q4 2026", hint: "Oct 1 – Dec 31" },
  { value: "member-1", label: "Jesper", group: "Members" },
];

function SingleHarness({ onAction, fieldLabel }: { onAction?: (query: string) => void; fieldLabel?: string }) {
  const [value, setValue] = useState("");
  return <SearchSelect label="Period" fieldLabel={fieldLabel} options={options} value={value} onChange={setValue} clearLabel="All periods" actionLabel="Plan next period" onAction={onAction} />;
}

function MultiHarness({ onValuesChange }: { onValuesChange: (values: string[]) => void }) {
  const [values, setValues] = useState<string[]>([]);
  return <SearchSelect label="Projects" multiple options={options} values={values} onValuesChange={(next) => { setValues(next); onValuesChange(next); }} />;
}

describe("SearchSelect", () => {
  it("opens, searches and selects a single value", () => {
    render(<SingleHarness />);
    fireEvent.click(screen.getByRole("button", { name: "Period" }));
    fireEvent.change(screen.getByLabelText("Search Period"), { target: { value: "q4" } });
    expect(screen.queryByRole("option", { name: /Q3 2026/ })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("option", { name: /Q4 2026/ }));
    expect(screen.getByRole("button", { name: "Period" })).toHaveTextContent("Q4 2026");
    expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
  });

  it("clears the value via the clear option", () => {
    render(<SingleHarness />);
    fireEvent.click(screen.getByRole("button", { name: "Period" }));
    fireEvent.click(screen.getByRole("option", { name: /Q3 2026/ }));
    fireEvent.click(screen.getByRole("button", { name: "Period" }));
    fireEvent.click(screen.getByRole("option", { name: "All periods" }));
    expect(screen.getByRole("button", { name: "Period" })).toHaveTextContent("Select…");
  });

  it("toggles multiple values and stays open", () => {
    const spy = vi.fn();
    render(<MultiHarness onValuesChange={spy} />);
    fireEvent.click(screen.getByRole("button", { name: "Projects" }));
    fireEvent.click(screen.getByRole("option", { name: /Q3 2026/ }));
    fireEvent.click(screen.getByRole("option", { name: /Q4 2026/ }));
    expect(spy).toHaveBeenLastCalledWith(["period-1", "period-2"]);
    expect(screen.getByRole("listbox")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("option", { name: /Q3 2026/ }));
    expect(spy).toHaveBeenLastCalledWith(["period-2"]);
  });

  it("renders group headers", () => {
    render(<SingleHarness />);
    fireEvent.click(screen.getByRole("button", { name: "Period" }));
    expect(screen.getByText("Members")).toBeInTheDocument();
  });

  // FIR-3589 item 3: picking a value must not wipe out the field's own name.
  it("keeps the field label visible after a value is picked", () => {
    render(<SingleHarness fieldLabel="Period" />);
    const trigger = screen.getByRole("button", { name: "Period" });
    expect(trigger).toHaveTextContent("Period");
    fireEvent.click(trigger);
    fireEvent.click(screen.getByRole("option", { name: /Q4 2026/ }));
    expect(trigger).toHaveTextContent("Period");
    expect(trigger).toHaveTextContent("Q4 2026");
  });

  // FIR-3589 item 11: on a phone there is no "click outside" affordance.
  it("closes from the explicit close control", () => {
    render(<SingleHarness />);
    fireEvent.click(screen.getByRole("button", { name: "Period" }));
    expect(screen.getByRole("listbox")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Close Period" }));
    expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
  });

  it("closes a multi-select from Done", () => {
    render(<MultiHarness onValuesChange={vi.fn()} />);
    fireEvent.click(screen.getByRole("button", { name: "Projects" }));
    fireEvent.click(screen.getByRole("option", { name: /Q3 2026/ }));
    expect(screen.getByRole("listbox")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Done" }));
    expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
  });

  it("fires the bottom action with the current query", () => {
    const action = vi.fn();
    render(<SingleHarness onAction={action} />);
    fireEvent.click(screen.getByRole("button", { name: "Period" }));
    fireEvent.change(screen.getByLabelText("Search Period"), { target: { value: "2027" } });
    fireEvent.click(screen.getByRole("button", { name: /Plan next period/ }));
    expect(action).toHaveBeenCalledWith("2027");
    expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
  });
});
