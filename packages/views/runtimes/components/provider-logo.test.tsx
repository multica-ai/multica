import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ProviderLogo } from "./provider-logo";

describe("ProviderLogo", () => {
  it("renders the dedicated Qwen Code mark", () => {
    const { container } = render(<ProviderLogo provider="qwen" className="runtime-logo" />);

    const logo = container.querySelector('img[aria-hidden="true"]');
    const logoSrc = decodeURIComponent(logo?.getAttribute("src") ?? "");

    expect(logo?.getAttribute("alt")).toBe("");
    expect(logoSrc).toContain("viewBox='0 0 141.38 140'");
    expect(logoSrc).toContain("fill='#6D44E8'");
    expect(logo?.classList.contains("runtime-logo")).toBe(true);
  });
});
