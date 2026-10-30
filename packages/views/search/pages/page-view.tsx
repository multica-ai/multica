"use client";

import type { DelegateActor, PalettePage } from "../types";
import {
  IssueAssigneePage,
  IssueDueDatePage,
  IssueLabelsPage,
  IssueParentPage,
  IssuePriorityPage,
  IssueProjectPage,
  IssueQuickActionPage,
  IssueStatusPage,
} from "./issue-pages";
import {
  AgentAssignIssuePage,
  ChatProjectPage,
  DelegateTargetPage,
  ProjectStatusPage,
  ThemePage,
} from "./other-pages";

export function PageView({
  page,
  query,
  onDelegate,
}: {
  page: PalettePage;
  query: string;
  onDelegate: (actor: DelegateActor, prompt: string) => void;
}) {
  switch (page.type) {
    case "issue-status":
      return <IssueStatusPage target={page.target} query={query} />;
    case "issue-assignee":
      return <IssueAssigneePage target={page.target} query={query} />;
    case "issue-priority":
      return <IssuePriorityPage target={page.target} query={query} />;
    case "issue-labels":
      return <IssueLabelsPage target={page.target} query={query} />;
    case "issue-due-date":
      return <IssueDueDatePage target={page.target} query={query} />;
    case "issue-project":
      return <IssueProjectPage target={page.target} query={query} />;
    case "issue-parent":
      return <IssueParentPage target={page.target} query={query} />;
    case "issue-quick-action":
      return <IssueQuickActionPage issue={page.issue} query={query} />;
    case "theme":
      return <ThemePage query={query} />;
    case "project-status":
      return <ProjectStatusPage project={page.project} query={query} />;
    case "agent-assign-issue":
      return <AgentAssignIssuePage agent={page.agent} query={query} />;
    case "chat-project":
      return <ChatProjectPage session={page.session} changeProject={page.changeProject} query={query} />;
    case "delegate-target":
      return <DelegateTargetPage query={query} onSubmit={(actor) => onDelegate(actor, page.prompt)} />;
  }
}
