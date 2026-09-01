import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import enLayout from "../../locales/en/layout.json";

const navigation = vi.hoisted(() => ({
  current: { pathname: "/acme/runtimes", push: vi.fn() },
}));

vi.mock("../../navigation", () => ({
  useNavigation: () => navigation.current,
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    agents: () => "/acme/agents",
    runtimes: () => "/acme/runtimes",
  }),
}));

vi.mock("../../i18n", () => ({
  useT: () => ({
    t: (pick: (ns: typeof enLayout) => string) => pick(enLayout),
  }),
}));

vi.mock("../../agents/components/agents-page", () => ({
  AgentsPage: () => <div>agents-panel</div>,
}));

vi.mock("./runtimes-page", () => ({
  RuntimesPage: () => <div>runtimes-panel</div>,
}));

import { AgentsRuntimesPage } from "./agents-runtimes-page";

describe("AgentsRuntimesPage", () => {
  beforeEach(() => {
    navigation.current = { pathname: "/acme/runtimes", push: vi.fn() };
  });

  it("shows the runtimes panel on the runtimes route", () => {
    render(<AgentsRuntimesPage />);
    expect(screen.getByText("runtimes-panel")).toBeInTheDocument();
    expect(screen.queryByText("agents-panel")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Runtimes" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
  });

  it("shows the agents panel on the agents route", () => {
    navigation.current.pathname = "/acme/agents";
    render(<AgentsRuntimesPage />);
    expect(screen.getByText("agents-panel")).toBeInTheDocument();
    expect(screen.queryByText("runtimes-panel")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Agents" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
  });

  it("navigates between the merged panels", async () => {
    const user = userEvent.setup();
    render(<AgentsRuntimesPage />);
    await user.click(screen.getByRole("button", { name: "Agents" }));
    expect(navigation.current.push).toHaveBeenCalledWith("/acme/agents");
  });
});
