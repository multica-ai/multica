import { describe, expect, it } from "vitest";
import { fireEvent, render, screen, within } from "@testing-library/react";
import type { ToolPolicyRow } from "../core";
import { CapabilityCatalog, groupByCapability } from "./capability-catalog";

const row = (over: Partial<ToolPolicyRow> = {}): ToolPolicyRow => ({
  tool_key: "read_files",
  resource_pattern: "",
  title: "Read files",
  category: "tools",
  source: "scan",
  managed_externally: false,
  layers: { workspace: null, runtime: null, agent: null, group: null, user: null, system: null },
  conditions: { workspace: null, runtime: null, agent: null, user: null, system: null },
  effective: { setting: "allow", decided_by: "", capped_by: "", reason: "" },
  capped_by_groups: [],
  ...over,
});

describe("groupByCapability", () => {
  it("groups a free-form category into its own card and keeps generic buckets under the type", () => {
    const rows = [
      // generic "tools" bucket → falls back to the Multica type
      row({ tool_key: "create_issue", category: "tools", source: "multica" }),
      // real domain label → its own card
      row({ tool_key: "slack_post", category: "Slack", source: "multica" }),
      row({ tool_key: "slack_read", category: "Slack", source: "multica" }),
    ];
    const groups = groupByCapability(rows);
    const labels = groups.map((g) => g.label).sort();
    expect(labels).toContain("Slack");
    expect(labels).toContain("Multica");
    const slack = groups.find((g) => g.label === "Slack")!;
    expect(slack.rows.map((r) => r.tool_key)).toEqual(["slack_post", "slack_read"]);
  });

  it("orders cards by permission type, not by verdict", () => {
    const rows = [
      row({ tool_key: "conn_a", category: "Slack", source: "connection" }),
      row({ tool_key: "mul_a", category: "tools", source: "multica" }),
      row({ tool_key: "rt_a", category: "Runtimes", source: "runtime_report" }),
    ];
    const groups = groupByCapability(rows);
    // Multica < Runtime < Connections in TYPE_ORDER.
    expect(groups.map((g) => g.type)).toEqual([
      "Multica",
      "Runtime",
      "Connections",
    ]);
  });

  it("counts rows per Effective verdict for the header dot summary", () => {
    const rows = [
      row({ tool_key: "a", category: "Slack", effective: { setting: "allow", decided_by: "", capped_by: "", reason: "" } }),
      row({ tool_key: "b", category: "Slack", effective: { setting: "ask", decided_by: "", capped_by: "", reason: "" } }),
      row({ tool_key: "c", category: "Slack", effective: { setting: "deny", decided_by: "", capped_by: "", reason: "" } }),
      row({ tool_key: "d", category: "Slack", effective: { setting: "allow", decided_by: "", capped_by: "", reason: "" } }),
    ];
    const [group] = groupByCapability(rows);
    expect(group!.counts).toEqual({ allow: 2, ask: 1, deny: 1 });
  });
});

describe("CapabilityCatalog", () => {
  it("renders a card per group and delegates the decision cell to the caller", () => {
    const rows = [
      row({ tool_key: "slack_post", title: "Post to Slack", category: "Slack", source: "multica" }),
      row({ tool_key: "create_issue", title: "Create issue", category: "tools", source: "multica" }),
    ];
    render(
      <CapabilityCatalog
        rows={rows}
        renderDecision={(r) => <button type="button">decide:{r.tool_key}</button>}
      />,
    );
    // The caller's decision cell shows for each row.
    expect(screen.getByText("decide:slack_post")).toBeInTheDocument();
    expect(screen.getByText("decide:create_issue")).toBeInTheDocument();
    // Rows land in the expected cards.
    const slackCard = screen.getByTestId("capability-group-slack");
    expect(within(slackCard).getByText("Post to Slack")).toBeInTheDocument();
    expect(within(slackCard).getByText("slack_post")).toBeInTheDocument();
  });

  it("expands a group row inline to reveal its detail, and leaves leaf rows flat (FIR-2706)", () => {
    const rows = [
      // a connection row that has an inline group of underlying tools
      row({ tool_key: "connection:acme", title: "Acme", category: "Acme", source: "connection" }),
      // a plain leaf row with no underlying group
      row({ tool_key: "create_issue", title: "Create issue", category: "tools", source: "multica" }),
    ];
    render(
      <CapabilityCatalog
        rows={rows}
        renderDecision={(r) => <button type="button">decide:{r.tool_key}</button>}
        renderDetail={(r) =>
          r.source === "connection" ? <div>group-of:{r.tool_key}</div> : null
        }
      />,
    );
    // The leaf row shows no expand affordance…
    expect(screen.queryByTestId("tool-expand-create_issue")).not.toBeInTheDocument();
    // …the group row does, and its detail is hidden until expanded.
    const expander = screen.getByTestId("tool-expand-connection:acme");
    expect(expander).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByText("group-of:connection:acme")).not.toBeInTheDocument();
    // Clicking the row reveals the inline group; clicking again collapses it.
    fireEvent.click(expander);
    expect(expander).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByText("group-of:connection:acme")).toBeInTheDocument();
    fireEvent.click(expander);
    expect(screen.queryByText("group-of:connection:acme")).not.toBeInTheDocument();
  });
});
