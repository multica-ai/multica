import { afterEach, expect, it, vi } from "vitest";
import { cleanup, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithI18n } from "../../../../test/i18n";
import { graphFixture, graphId, graphNode } from "./graph-fixture";
import { GraphRunLocator } from "./graph-run-locator";
afterEach(cleanup);
it("positions the latest of 22 terminal Runs without claiming one is running", async () => {
  const fixture = graphFixture(1), locate = vi.fn();
  const runs = Array.from({ length: 22 }, (_, n) => ({ ...graphNode("run", 100 + n, 10, "History"), status: n % 2 ? "completed" : "cancelled", created_at: new Date(Date.UTC(2026, 0, 1, 0, n)).toISOString() }));
  renderWithI18n(<GraphRunLocator graph={{ ...fixture.list, nodes: [...fixture.list.nodes, ...runs] }} issueId={graphId(10)} locate={locate}/>);
  expect(screen.getByRole("button", { name: "Locate running" })).toBeDisabled();
  expect(screen.getByText("No running runs loaded")).toBeVisible();
  expect(locate).not.toHaveBeenCalled();
  await userEvent.setup().click(screen.getByRole("button", { name: "Locate latest record" }));
  expect(locate).toHaveBeenCalledWith("run:" + graphId(121));
});
it("positions all parallel running Runs individually while ignoring another owner", async () => {
  const fixture = graphFixture(1), locate = vi.fn();
  const runs = [graphNode("run", 20, 10, "First"), graphNode("run", 21, 10, "Second"), graphNode("run", 22, 11, "Other")].map(node => ({ ...node, status: "running" }));
  renderWithI18n(<GraphRunLocator graph={{ ...fixture.list, nodes: [...fixture.list.nodes, ...runs] }} issueId={graphId(10)} locate={locate}/>);
  const user = userEvent.setup();
  await user.click(screen.getByRole("button", { name: "Locate running" }));
  await user.click(screen.getByRole("button", { name: "Locate running" }));
  expect(locate.mock.calls).toEqual([["run:" + graphId(20)], ["run:" + graphId(21)]]);
});
it("keeps zero loaded Runs explicit without selecting anything", () => {
  const fixture = graphFixture(1), locate = vi.fn();
  renderWithI18n(<GraphRunLocator graph={fixture.list} issueId={graphId(10)} locate={locate}/>);
  expect(screen.getByText("No runs loaded")).toBeVisible();
  expect(screen.getByRole("button", { name: "Locate latest record" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Locate running" })).toBeDisabled();
  expect(locate).not.toHaveBeenCalled();
});
