// @vitest-environment jsdom

import { describe, it, expect, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import enAgents from "../../locales/en/agents.json";
import { AgentLifecycleSection } from "./agent-lifecycle-section";

function renderSection(archived: boolean) {
  const onArchive = vi.fn();
  const onRestore = vi.fn();
  render(
    <I18nProvider locale="en" resources={{ en: { agents: enAgents } }}>
      <AgentLifecycleSection
        archived={archived}
        onArchive={onArchive}
        onRestore={onRestore}
      />
    </I18nProvider>,
  );
  return { onArchive, onRestore };
}

describe("AgentLifecycleSection", () => {
  it("offers Archive in a danger zone for an active agent", () => {
    const { onArchive, onRestore } = renderSection(false);
    expect(screen.getByText("Danger zone")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Archive" }));
    expect(onArchive).toHaveBeenCalledOnce();
    expect(onRestore).not.toHaveBeenCalled();
    expect(screen.queryByRole("button", { name: "Restore" })).not.toBeInTheDocument();
  });

  it("offers Restore for an archived agent", () => {
    const { onArchive, onRestore } = renderSection(true);
    expect(screen.queryByText("Danger zone")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Restore" }));
    expect(onRestore).toHaveBeenCalledOnce();
    expect(onArchive).not.toHaveBeenCalled();
  });
});
