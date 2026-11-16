import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import {
  Select,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";

describe("Select", () => {
  it("renders the selected item label instead of its raw value", () => {
    render(
      <Select
        items={[
          { value: "select", label: "Single select" },
          { value: "multi_select", label: "Multi-select" },
        ]}
        value="multi_select"
      >
        <SelectTrigger aria-label="Property type">
          <SelectValue />
        </SelectTrigger>
      </Select>,
    );

    const trigger = screen.getByRole("combobox", { name: "Property type" });
    expect(trigger).toHaveTextContent("Multi-select");
    expect(trigger).not.toHaveTextContent("multi_select");
  });
});
