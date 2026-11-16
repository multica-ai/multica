import {
  BarChart3,
  Bug,
  FileSearch,
  GitPullRequest,
  Newspaper,
  Shield,
  type IconComponent,
} from "@multica/ui/icons";
import type { ScheduleConfig } from "./schedule-editor/model";

// Templates offered by the empty state and the Autopilots panel.
// Prompts stay raw English because they're injected directly into the
// agent's task input.

export type AutopilotTemplateId =
  | "daily_news"
  | "pr_review"
  | "bug_triage"
  | "weekly_progress"
  | "dependency_audit"
  | "documentation_check";

export interface AutopilotTemplate {
  id: AutopilotTemplateId;
  prompt: string;
  icon: IconComponent;
  schedule: Pick<ScheduleConfig, "time" | "days">;
}

const WEEKDAYS: ScheduleConfig["days"] = {
  kind: "weekly",
  daysOfWeek: [1, 2, 3, 4, 5],
};
const MONDAY: ScheduleConfig["days"] = { kind: "weekly", daysOfWeek: [1] };

export const AUTOPILOT_TEMPLATES: AutopilotTemplate[] = [
  {
    id: "daily_news",
    prompt: `1. Search the web for news and announcements published today only (strictly today's date)
2. Filter for topics relevant to our team and industry
3. For each item, write a short summary including: title, source, key takeaways
4. Compile everything into a single digest post
5. Post the digest as a comment on this issue and @mention all workspace members`,
    icon: Newspaper,
    schedule: { time: { kind: "at", time: "09:00" }, days: { kind: "every" } },
  },
  {
    id: "pr_review",
    prompt: `1. List all open pull requests in the repository
2. Identify PRs that have been open for more than 24 hours without a review
3. For each stale PR, note the author, age, and a one-line summary of the change
4. Post a comment on this issue listing all stale PRs with links
5. @mention the team to remind them to review`,
    icon: GitPullRequest,
    schedule: { time: { kind: "at", time: "10:00" }, days: WEEKDAYS },
  },
  {
    id: "bug_triage",
    prompt: `1. List all backlog issues that have not been prioritized
2. For each issue, read the description and any attached logs or screenshots
3. Assess severity (critical / high / medium / low) based on user impact and scope
4. Set the priority field on the issue accordingly
5. Add a comment explaining your assessment and suggested next steps`,
    icon: Bug,
    schedule: { time: { kind: "at", time: "09:00" }, days: WEEKDAYS },
  },
  {
    id: "weekly_progress",
    prompt: `1. Gather all issues completed (status "done") in the past 7 days
2. Gather all issues currently in progress
3. Identify any blocked issues and their blockers
4. Calculate key metrics: issues closed, issues opened, net change
5. Write a structured weekly report with sections: Completed, In Progress, Blocked, Metrics
6. Post the report as a comment on this issue`,
    icon: BarChart3,
    schedule: { time: { kind: "at", time: "17:00" }, days: MONDAY },
  },
  {
    id: "dependency_audit",
    prompt: `1. Run dependency audit tools on the project (npm audit, go vuln check, etc.)
2. Identify any packages with known security vulnerabilities
3. List outdated packages that are more than 2 major versions behind
4. For each finding, note the severity, affected package, and recommended fix
5. Post a summary report as a comment with actionable items`,
    icon: Shield,
    schedule: { time: { kind: "at", time: "08:00" }, days: MONDAY },
  },
  {
    id: "documentation_check",
    prompt: `1. List all code changes merged in the past 7 days (via git log)
2. For each significant change, check if related documentation was updated
3. Identify any new APIs, config options, or features missing documentation
4. Create a list of documentation gaps with file paths and suggested content
5. Post the findings as a comment on this issue`,
    icon: FileSearch,
    schedule: { time: { kind: "at", time: "14:00" }, days: MONDAY },
  },
];
