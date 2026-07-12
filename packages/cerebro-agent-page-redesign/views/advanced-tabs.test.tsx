import { expect, it } from "vitest";
import { useState } from "react";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { advancedTabs, type RedesignTab } from "./agent-page-tabs";
import { AdvancedTabs } from "./advanced-tabs";

function Harness({ mcpConfig = true }: { mcpConfig?: boolean }) {
  const [active, setActive] = useState<RedesignTab>("infisical");
  return (
    <AdvancedTabs
      tabs={advancedTabs({ mcpConfig })}
      active={active}
      onSelect={setActive}
      renderContent={(tab) => <div>{tab} content</div>}
    />
  );
}

it("switches between the grouped advanced settings", async () => {
  const user = userEvent.setup();
  render(<Harness />);

  expect(screen.getByRole("tabpanel")).toHaveTextContent("infisical content");
  await user.click(screen.getByRole("tab", { name: "Sandbox" }));
  expect(screen.getByRole("tabpanel")).toHaveTextContent("sandbox content");
  await user.click(screen.getByRole("tab", { name: "MCP Config" }));
  expect(screen.getByRole("tabpanel")).toHaveTextContent("mcp_config content");
});

it("omits MCP Config when the runtime does not support it", () => {
  render(<Harness mcpConfig={false} />);
  expect(screen.queryByRole("tab", { name: "MCP Config" })).not.toBeInTheDocument();
});
