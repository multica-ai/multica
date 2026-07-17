// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { RuntimeSettingSearchSelect } from "./runtime-setting-search-select";

afterEach(cleanup);

const options = [
  { value: "low", label: "Low", description: "Faster responses" },
  { value: "high", label: "High", description: "More reasoning" },
];

describe("RuntimeSettingSearchSelect", () => {
  it("opens from the displayed property text, filters, and selects an option", () => {
    const onChange = vi.fn();
    render(
      <RuntimeSettingSearchSelect
        variant="property"
        ariaLabel="Effort"
        value="low"
        options={options}
        defaultOption={{ value: "", label: "Runtime default" }}
        searchPlaceholder="Search effort"
        onChange={onChange}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Effort: Low" }));
    const search = screen.getByRole("textbox", { name: "Search effort" });
    fireEvent.change(search, { target: { value: "high" } });

    expect(screen.queryByText("Faster responses")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: /High/ }));
    expect(onChange).toHaveBeenCalledWith("high");
  });

  it("uses the Model form trigger shape and can select the runtime default", () => {
    const onChange = vi.fn();
    render(
      <RuntimeSettingSearchSelect
        variant="form"
        ariaLabel="Speed"
        value="fast"
        options={[{ value: "standard", label: "Standard" }, { value: "fast", label: "Fast" }]}
        defaultOption={{ value: "", label: "Runtime default" }}
        searchPlaceholder="Search speed"
        onChange={onChange}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Speed: Fast" }));
    fireEvent.click(screen.getByRole("button", { name: "Runtime default" }));
    expect(onChange).toHaveBeenCalledWith("");
  });
});
