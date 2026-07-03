import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import type { ChatTimelineItem } from "@multica/core/chat";
import enChat from "../../../locales/en/chat.json";
import { ReadToolBody } from "./read";

const RESOURCES = { en: { chat: enChat } };

function renderRead(item: ChatTimelineItem) {
  return render(
    <I18nProvider locale="en" resources={RESOURCES}>
      <ReadToolBody item={item} />
    </I18nProvider>,
  );
}

function readItem(input: Record<string, unknown>, output = "file contents"): ChatTimelineItem {
  return { seq: 1, type: "tool_use", tool: "Read", input, output, status: "done" };
}

describe("ReadToolBody", () => {
  it("shows an explicit line range from offset + limit", () => {
    renderRead(readItem({ file_path: "/a/b.ts", offset: 10, limit: 20 }));
    expect(screen.getByText("lines 10–29")).toBeInTheDocument();
  });

  it("shows a first-N range when only limit is given", () => {
    renderRead(readItem({ file_path: "/a/b.ts", limit: 50 }));
    expect(screen.getByText("first 50 lines")).toBeInTheDocument();
  });

  it("offers a collapsed content preview", () => {
    renderRead(readItem({ file_path: "/a/b.ts" }, "hello"));
    expect(screen.getByText("preview")).toBeInTheDocument();
  });
});
