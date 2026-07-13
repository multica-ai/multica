// @vitest-environment jsdom

import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  flags: new Map<string, boolean>(),
  mode: "classic" as "classic" | "dynamic",
}));

vi.mock("@multica/cerebro-feature-flags", () => ({
  useFeatureFlag: (key: string) => mocks.flags.get(key) ?? false,
}));

vi.mock("@multica/cerebro-inbox", () => ({
  useInboxMode: () => mocks.mode,
}));

vi.mock("@multica/views/inbox", () => ({
  InboxPage: () => <div>Classic inbox</div>,
}));

vi.mock("./dynamic-inbox", () => ({
  DynamicInbox: () => <div>Dynamic inbox</div>,
}));

import { CerebroInboxSwitcher } from "./cerebro-inbox-switcher";

describe("CerebroInboxSwitcher", () => {
  beforeEach(() => {
    mocks.flags.clear();
    mocks.mode = "classic";
  });

  it("renders the rounds-capable inbox when inbox rounds are enabled", () => {
    mocks.flags.set("cerebro_inbox_rounds", true);

    render(<CerebroInboxSwitcher />);

    expect(screen.getByText("Dynamic inbox")).toBeTruthy();
  });
});
