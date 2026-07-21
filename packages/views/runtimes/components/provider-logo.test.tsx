import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ProviderLogo } from "./provider-logo";

describe("ProviderLogo", () => {
  it("renders the dedicated Qwen Code mark", () => {
    const { container } = render(<ProviderLogo provider="qwen" className="runtime-logo" />);

    const logo = container.querySelector('svg[viewBox="0 0 141.38 140"]');

    expect(logo?.querySelector('path[fill="#6D44E8"]')).not.toBeNull();
    expect(logo?.classList.contains("runtime-logo")).toBe(true);
  });
});
