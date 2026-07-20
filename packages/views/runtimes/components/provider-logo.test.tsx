import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ProviderLogo } from "./provider-logo";

describe("ProviderLogo", () => {
  it("renders the dedicated Qwen Code mark", () => {
    const { container } = render(<ProviderLogo provider="qwen" className="runtime-logo" />);

    expect(container.querySelector("#qwen-logo-gradient")).not.toBeNull();
    expect(container.querySelector("svg")?.classList.contains("runtime-logo")).toBe(true);
  });
});
