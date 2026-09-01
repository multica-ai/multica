import { readFileSync } from "node:fs";
import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ProviderLogo } from "./provider-logo";

describe("ProviderLogo", () => {
  it("renders the dedicated CodeArts icon", () => {
    const { container } = render(
      <ProviderLogo provider="codearts" className="runtime-logo" />,
    );

    const logo = container.querySelector('img[alt="CodeArts"]');

    expect(logo?.getAttribute("src")).toBeTruthy();
    expect(logo?.classList.contains("runtime-logo")).toBe(true);
  });

  it("keeps the official Reasonix artwork", () => {
    const logoSvg = readFileSync("runtimes/components/reasonix-logo.svg", "utf8");

    expect(logoSvg).toContain('viewBox="0 0 64 64"');
    expect(logoSvg).toContain('stop-color="#4f9dff"');
    expect(logoSvg).toContain('stop-color="#c46bff"');
  });

  it("renders the official Reasonix logo", () => {
    const { container } = render(
      <ProviderLogo provider="reasonix" className="runtime-logo" />,
    );

    const logo = container.querySelector('img[alt="Reasonix"]');

    expect(logo?.getAttribute("src")).toBeTruthy();
    expect(logo?.classList.contains("runtime-logo")).toBe(true);
  });

  it("renders the dedicated Dim mark", () => {
    const { container } = render(<ProviderLogo provider="dim" className="runtime-logo" />);

    const logo = container.querySelector("img");
    const logoSrc = decodeURIComponent(logo?.getAttribute("src") ?? "");

    expect(logo?.getAttribute("alt")).toBe("");
    expect(logoSrc).toContain("dim-logo.png");
    expect(logo?.classList.contains("runtime-logo")).toBe(true);
  });

  it("renders the dedicated Qwen Code mark", () => {
    const { container } = render(<ProviderLogo provider="qwen" className="runtime-logo" />);

    const logo = container.querySelector('img[aria-hidden="true"]');
    const logoSrc = decodeURIComponent(logo?.getAttribute("src") ?? "");

    expect(logo?.getAttribute("alt")).toBe("");
    expect(logoSrc).toContain("viewBox='0 0 141.38 140'");
    expect(logoSrc).toContain("fill='#6D44E8'");
    expect(logo?.classList.contains("runtime-logo")).toBe(true);
  });

  it("renders the official DeepSeek Harness mark instead of the generic fallback", () => {
    const { container } = render(
      <ProviderLogo provider="dsh" className="runtime-logo" />,
    );

    const logo = container.querySelector("svg");

    // Inlined rather than loaded through <img>: currentColor only resolves
    // against the host document, so an <img> would pin the mark to black and
    // lose it against the dark theme.
    expect(logo?.getAttribute("viewBox")).toBe("0 0 50 50");
    expect(logo?.getAttribute("fill")).toBe("currentColor");
    expect(logo?.querySelector("path")).not.toBeNull();
    expect(logo?.classList.contains("runtime-logo")).toBe(true);
  });

  it("renders the QwenPaw mark instead of the generic fallback", () => {
    const { container } = render(
      <ProviderLogo provider="qwenpaw" className="runtime-logo" />,
    );

    const logo = container.querySelector("svg");

    // currentColor keeps the single path legible in both themes, matching the
    // separate light/dark marks upstream ships.
    expect(logo?.getAttribute("fill")).toBe("currentColor");
    expect(logo?.querySelector("path")).not.toBeNull();
    expect(logo?.classList.contains("runtime-logo")).toBe(true);
  });

  it("renders the MiniMax Code mark instead of the generic fallback", () => {
    const { container } = render(
      <ProviderLogo provider="mcode" className="runtime-logo" />,
    );

    const logo = container.querySelector("svg");
    const path = logo?.querySelector("path");

    // Official MiniMax Code compound path: clipped-corner card + inner "m".
    // currentColor follows the theme instead of pinning the mark to black.
    expect(logo?.getAttribute("viewBox")).toBe("2.1 2 27.9 28");
    expect(logo?.getAttribute("fill")).toBe("currentColor");
    expect(path?.getAttribute("d")).toContain("M27.0157 5.80436");
    expect(path?.getAttribute("d")).toContain("ZM11.0587 8.88053");
    expect(logo?.classList.contains("runtime-logo")).toBe(true);
  });

  it("renders the ZeroClaw placeholder mark instead of the generic fallback", () => {
    const { container } = render(
      <ProviderLogo provider="zeroclaw" className="runtime-logo" />,
    );

    const logo = container.querySelector("svg");

    // No official ZeroClaw asset has been sourced yet, so this pins the
    // deliberate placeholder mark (three strokes) rather than the generic
    // <Monitor /> fallback that unknown providers get.
    expect(logo?.getAttribute("viewBox")).toBe("0 0 24 24");
    expect(logo?.getAttribute("stroke")).toBe("currentColor");
    expect(logo?.querySelectorAll("path").length).toBe(3);
    expect(logo?.classList.contains("runtime-logo")).toBe(true);
  });
});
