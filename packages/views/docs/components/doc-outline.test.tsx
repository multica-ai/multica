import { fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import enDocs from "../../locales/en/docs.json";
import { DocOutline, readOutline } from "./doc-outline";

function I18nWrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={{ en: { docs: enDocs } }}>
      {children}
    </I18nProvider>
  );
}

function renderedDoc(html: string) {
  const root = document.createElement("div");
  root.innerHTML = `<div class="ProseMirror">${html}</div>`;
  return root;
}

describe("readOutline", () => {
  it("lists top-level H1–H3 in order and skips empty headings", () => {
    const root = renderedDoc(
      "<h1>Overview</h1><p>x</p><h2>Setup</h2><h4>Deep</h4><h3> </h3><blockquote><h2>Quoted</h2></blockquote><h3>Verify</h3>",
    );
    expect(readOutline(root).map(({ level, text }) => ({ level, text }))).toEqual([
      { level: 1, text: "Overview" },
      { level: 2, text: "Setup" },
      { level: 3, text: "Verify" },
    ]);
  });
});

describe("readOutline: hidden editor", () => {
  it("skips an editor still mounting behind `hidden`", () => {
    const root = document.createElement("div");
    root.innerHTML =
      '<div class="ProseMirror"><h1>Shown</h1></div><div hidden><div class="ProseMirror"><h1>Mounting</h1></div></div>';
    expect(readOutline(root).map(({ text }) => text)).toEqual(["Shown"]);
  });
});

describe("DocOutline", () => {
  it("marks the current heading and reports clicks", () => {
    const headings = readOutline(
      renderedDoc("<h2>Goals</h2><h3>Scope</h3><h2>Plan</h2>"),
    );
    const onSelect = vi.fn();
    render(<DocOutline headings={headings} activeIndex={1} onSelect={onSelect} />, {
      wrapper: I18nWrapper,
    });
    expect(screen.getByRole("navigation", { name: "Outline" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Scope" })).toHaveAttribute(
      "aria-current",
      "location",
    );
    fireEvent.click(screen.getByRole("button", { name: "Plan" }));
    expect(onSelect).toHaveBeenCalledWith(headings[2]);
  });
});
