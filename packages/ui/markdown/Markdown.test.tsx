import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { Markdown } from "./Markdown";

vi.mock("react-i18next", () => ({
  useTranslation: () => ({
    t: () => "Plain text",
  }),
}));

describe("Markdown sanitize schema", () => {
  afterEach(() => {
    cleanup();
  });

  it("renders <mark> tags from raw HTML without stripping them", () => {
    const { container } = render(
      <Markdown mode="minimal">{`This is <mark>highlighted</mark> text`}</Markdown>,
    );

    const markEl = container.querySelector("mark");
    expect(markEl).not.toBeNull();
    expect(markEl?.textContent).toBe("highlighted");
  });

  it("does not render disallowed HTML tags", () => {
    const { container } = render(
      <Markdown mode="minimal">{`This is <script>alert('xss')</script> text`}</Markdown>,
    );

    const scriptEl = container.querySelector("script");
    expect(scriptEl).toBeNull();
  });
});
