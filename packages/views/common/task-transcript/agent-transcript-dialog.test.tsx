import type { ReactNode } from "react";
import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { AgentTranscriptDialog } from "./agent-transcript-dialog";
import type { AgentTask } from "@multica/core/types/agent";
import type { TimelineItem } from "./build-timeline";
import enAgents from "../../locales/en/agents.json";

const TEST_RESOURCES = { en: { agents: enAgents } };

function Wrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
}

const task = {
  id: "task-1",
  status: "completed",
  created_at: new Date().toISOString(),
} as AgentTask;

const items: TimelineItem[] = [
  { seq: 1, type: "text", content: "Done" },
];

describe("AgentTranscriptDialog", () => {
  it("renders timeline items through the shared component", () => {
    render(
      <QueryClientProvider client={new QueryClient()}>
        <AgentTranscriptDialog open task={task} items={items} agentName="Test Agent" onOpenChange={() => {}} />
      </QueryClientProvider>,
      { wrapper: Wrapper },
    );
    expect(screen.getByText("Done")).toBeInTheDocument();
  });
});
