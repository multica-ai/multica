import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
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

describe("actor property display", () => {
  it("collapses several people onto one line when compact", () => {
    renderDisplay(["a", "b", "c"], true);

    expect(screen.getAllByTestId("avatar")).toHaveLength(1);
    expect(screen.getByText("Ada Lovelace")).toBeInTheDocument();
    expect(screen.getByText("+2")).toBeInTheDocument();
    expect(screen.getByTitle("Ada Lovelace, Grace Hopper, Alan Turing")).not.toHaveClass("flex-wrap");
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
