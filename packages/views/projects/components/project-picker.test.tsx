import { describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import type { Project } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { ProjectPicker } from "./project-picker";

const longProjectName = "sqs-harness-module-backend-runtime-workdir";
let mockProjects: Project[] = [];

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({ data: mockProjects }),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "workspace-1",
}));

vi.mock("@multica/core/projects/queries", () => ({
  projectListOptions: () => ({ queryKey: ["projects"], queryFn: vi.fn() }),
}));

function makeProject(overrides: Partial<Project>): Project {
  return {
    id: "project-1",
    workspace_id: "workspace-1",
    title: "Project",
    description: "",
    icon: null,
    status: "planned",
    priority: "medium",
    lead_type: null,
    lead_id: null,
    issue_count: 0,
    done_count: 0,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    archived_at: null,
    archived_by: null,
    ...overrides,
  } as Project;
}

describe("ProjectPicker", () => {
  it("lets long project names wrap inside the dropdown", () => {
    mockProjects = [makeProject({ id: "project-long", title: longProjectName })];

    renderWithI18n(
      <ProjectPicker
        projectId={null}
        onUpdate={vi.fn()}
        defaultOpen
      />,
    );

    const optionLabel = screen.getByText(longProjectName);
    expect(optionLabel).not.toHaveClass("truncate");
    expect(optionLabel).toHaveClass("whitespace-normal");
  });
});
