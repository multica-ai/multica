import { describe, expect, it } from "vitest";
import { fireEvent, render } from "@testing-library/react";
import { ComposioToolkitLogo, composioToolkitLogoUrl } from "./composio-toolkit-logo";

describe("ComposioToolkitLogo", () => {
  it("builds Composio logo URLs from toolkit slugs", () => {
    expect(composioToolkitLogoUrl(" GitHub ")).toBe("https://logos.composio.dev/api/github");
    expect(composioToolkitLogoUrl("vercel", "dark")).toBe(
      "https://logos.composio.dev/api/vercel?theme=dark",
    );
  });

  it("uses backend logo for light mode and Composio dark logo for dark mode", () => {
    const { container } = render(
      <ComposioToolkitLogo
        slug="slack"
        name="Slack"
        fallbackLogo="https://cdn.example/slack.svg"
      />,
    );

    const images = Array.from(container.querySelectorAll("img"));
    expect(images).toHaveLength(2);
    expect(images[0]?.getAttribute("src")).toBe("https://cdn.example/slack.svg");
    expect(images[1]?.getAttribute("src")).toBe(
      "https://logos.composio.dev/api/slack?theme=dark",
    );
  });

  it("keeps the dark logo when the hidden light logo fails", () => {
    const { container } = render(<ComposioToolkitLogo slug="notion" name="Notion" />);

    const light = container.querySelector("img.dark\\:hidden");
    expect(light?.getAttribute("src")).toBe("https://logos.composio.dev/api/notion");

    fireEvent.error(light!);

    const dark = Array.from(container.querySelectorAll("img")).find((img) =>
      img.className.includes("dark:block"),
    );
    expect(dark?.getAttribute("src")).toBe(
      "https://logos.composio.dev/api/notion?theme=dark",
    );
  });
});
