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

function SingleHarness({ onAction }: { onAction?: (query: string) => void }) {
  const [value, setValue] = useState("");
  return <SearchSelect label="Period" options={options} value={value} onChange={setValue} clearLabel="All periods" actionLabel="Plan next period" onAction={onAction} />;
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
