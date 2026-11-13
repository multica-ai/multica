import { act, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import type { IssueProperty } from "@multica/core/types";
import enIssues from "../../../locales/en/issues.json";
import { CustomPropertyValueDisplay } from "./custom-property-picker";

const names: Record<string, string> = { a: "Ada Lovelace", b: "Grace Hopper", c: "Alan Turing" };

vi.mock("@multica/core/workspace/hooks", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/workspace/hooks")>()),
  useActorName: () => ({ getActorName: (_kind: string, id: string) => names[id] }),
}));
vi.mock("../../../common/actor-avatar", () => ({
  ActorAvatar: ({ actorId }: { actorId: string }) => <span data-testid="avatar">{actorId}</span>,
}));

const ref = (id: string) => `member:${id}`;
const property: IssueProperty = {
  id: "p-1", workspace_id: "ws-1", name: "Reviewers", type: "multi_actor",
  config: {}, position: 1, archived: false,
  created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z",
};

function renderDisplay(ids: string[], compact: boolean) {
  return render(
    <I18nProvider locale="en" resources={{ en: { issues: enIssues } }}>
      <CustomPropertyValueDisplay property={property} value={ids.map(ref)} compact={compact} />
    </I18nProvider>,
  );
}

const tooltip = () => document.querySelector("[data-slot=tooltip-content]");

function hover(el: HTMLElement) {
  vi.useFakeTimers();
  fireEvent.pointerEnter(el, { pointerType: "mouse" });
  act(() => {
    vi.advanceTimersByTime(550);
  });
}

describe("actor property display", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it("collapses several people onto one line when compact", () => {
    renderDisplay(["a", "b", "c"], true);

    expect(screen.getAllByTestId("avatar")).toHaveLength(1);
    expect(screen.getByText("Ada Lovelace")).toBeInTheDocument();
    expect(screen.getByText("+2")).toBeInTheDocument();
    expect(screen.getByRole("group", { name: "Ada Lovelace, Grace Hopper, Alan Turing" })).not.toHaveClass("flex-wrap");
  });

  it("names everyone on hover when some people are hidden", () => {
    renderDisplay(["a", "b", "c"], true);

    hover(screen.getByRole("group"));

    expect(tooltip()).toHaveTextContent("Ada Lovelace, Grace Hopper, Alan Turing");
  });

  it("names everyone on keyboard focus until focus leaves", async () => {
    const user = userEvent.setup();
    renderDisplay(["a", "b"], true);

    await user.tab();
    expect(screen.getByRole("group")).toHaveFocus();
    expect(tooltip()).toHaveTextContent("Ada Lovelace, Grace Hopper");

    await user.tab();
    expect(tooltip()).toBeNull();
  });

  it("keeps one person who fits quiet and out of the tab order", () => {
    renderDisplay(["a"], true);
    const pill = screen.getByRole("group");

    expect(pill).not.toHaveAttribute("tabindex");
    hover(pill);
    expect(tooltip()).toBeNull();
  });

  it("has no overflow count for one person", () => {
    renderDisplay(["a"], true);

    expect(screen.getByText("Ada Lovelace")).toBeInTheDocument();
    expect(screen.queryByText(/^\+/)).not.toBeInTheDocument();
  });

  it("still lists everyone when not compact", () => {
    renderDisplay(["a", "b"], false);

    expect(screen.getAllByTestId("avatar")).toHaveLength(2);
    expect(screen.getByText("Grace Hopper")).toBeInTheDocument();
  });
});
