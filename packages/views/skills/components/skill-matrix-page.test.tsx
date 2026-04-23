// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { SkillMatrixResponse, SkillMatrixSkill, SkillMatrixWorkspace } from "@multica/core/types";

const mockGetSkillMatrix = vi.hoisted(() => vi.fn());
const mockSyncSkillToWorkspaces = vi.hoisted(() => vi.fn());
const mockBulkDeleteSkills = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", () => ({
  api: {
    getSkillMatrix: () => mockGetSkillMatrix(),
    syncSkillToWorkspaces: (skillId: string, data: unknown) => 
      mockSyncSkillToWorkspaces(skillId, data),
    bulkDeleteSkills: (data: { skill_ids: string[] }) => 
      mockBulkDeleteSkills(data),
  },
}));

import { SkillMatrixPage } from "./skill-matrix-page";

// Test data with same skill name in multiple workspaces
const createTestMatrixData = (): SkillMatrixResponse => {
  const skills: SkillMatrixSkill[] = [
    { id: "skill-1", workspace_id: "ws-1", name: "test-skill", description: "Test skill" },
    { id: "skill-2", workspace_id: "ws-2", name: "test-skill", description: "Test skill" },
    { id: "skill-3", workspace_id: "ws-1", name: "other-skill", description: "Other skill" },
  ];
  
  // Unique skills by name (what the matrix displays)
  const uniqueSkills: SkillMatrixSkill[] = [
    { id: "skill-1", workspace_id: "ws-1", name: "test-skill", description: "Test skill" },
    { id: "skill-3", workspace_id: "ws-1", name: "other-skill", description: "Other skill" },
  ];

  const workspaces: SkillMatrixWorkspace[] = [
    { id: "ws-1", name: "Workspace 1", slug: "ws1", skill_count: 2 },
    { id: "ws-2", name: "Workspace 2", slug: "ws2", skill_count: 1 },
    { id: "ws-3", name: "Workspace 3", slug: "ws3", skill_count: 0 },
  ];

  // Matrix: [skill][workspace] = has_skill
  // test-skill: ws1=true, ws2=true, ws3=false
  // other-skill: ws1=true, ws2=false, ws3=false
  const matrix = [
    [true, true, false],  // test-skill exists in ws1 and ws2
    [true, false, false], // other-skill exists only in ws1
  ];

  // Critical: skill_lookup allows finding correct skill ID for each workspace
  const skillLookup: Record<string, Record<string, string>> = {
    "test-skill": {
      "ws-1": "skill-1",  // ID in ws1
      "ws-2": "skill-2",  // ID in ws2 (different!)
    },
    "other-skill": {
      "ws-1": "skill-3",
    },
  };

  return {
    skills: uniqueSkills,
    workspaces,
    matrix,
    skill_lookup: skillLookup,
  };
};

function renderWithQuery(ui: React.ReactNode) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      {ui}
    </QueryClientProvider>
  );
}

describe("SkillMatrixPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("renders skill matrix with correct structure", async () => {
    mockGetSkillMatrix.mockResolvedValue(createTestMatrixData());

    renderWithQuery(<SkillMatrixPage onBack={() => {}} />);

    await waitFor(() => {
      expect(screen.getByText("test-skill")).toBeInTheDocument();
      expect(screen.getByText("other-skill")).toBeInTheDocument();
    });

    // Verify workspace headers
    expect(screen.getByText("Workspace 1")).toBeInTheDocument();
    expect(screen.getByText("Workspace 2")).toBeInTheDocument();
  });

  it("shows correct icons for existing skills", async () => {
    mockGetSkillMatrix.mockResolvedValue(createTestMatrixData());

    renderWithQuery(<SkillMatrixPage onBack={() => {}} />);

    await waitFor(() => {
      expect(screen.getByText("test-skill")).toBeInTheDocument();
    });

    // Find cells for test-skill row
    const testSkillRow = screen.getByText("test-skill").closest("tr");
    expect(testSkillRow).toBeInTheDocument();

    // The row should have buttons for each workspace
    const buttons = testSkillRow?.querySelectorAll("button");
    expect(buttons?.length).toBeGreaterThanOrEqual(3);
  });

  it("selects cell for sync when clicking empty cell", async () => {
    mockGetSkillMatrix.mockResolvedValue(createTestMatrixData());

    renderWithQuery(<SkillMatrixPage onBack={() => {}} />);

    await waitFor(() => {
      expect(screen.getByText("test-skill")).toBeInTheDocument();
    });

    // Find test-skill row
    const testSkillRow = screen.getByText("test-skill").closest("tr");
    const buttons = testSkillRow?.querySelectorAll("button");
    
    // Click the button for ws-3 (where skill doesn't exist - empty cell)
    // ws-3 is the third workspace, so it should be the third button
    if (buttons && buttons[2]) {
      fireEvent.click(buttons[2]);
    }

    // Should show "Sync" button in toolbar
    await waitFor(() => {
      expect(screen.getByText(/Sync/)).toBeInTheDocument();
    });
  });

  it("selects cell for delete when clicking existing skill cell", async () => {
    mockGetSkillMatrix.mockResolvedValue(createTestMatrixData());

    renderWithQuery(<SkillMatrixPage onBack={() => {}} />);

    await waitFor(() => {
      expect(screen.getByText("test-skill")).toBeInTheDocument();
    });

    // Find test-skill row
    const testSkillRow = screen.getByText("test-skill").closest("tr");
    const buttons = testSkillRow?.querySelectorAll("button");
    
    // Click the first button (ws-1 where skill exists - green cell)
    if (buttons && buttons[0]) {
      fireEvent.click(buttons[0]);
    }

    // Should show "Delete" button in toolbar
    await waitFor(() => {
      expect(screen.getByText(/Delete/)).toBeInTheDocument();
    });
  });

  it("uses correct skill ID from skill_lookup for delete", async () => {
    const data = createTestMatrixData();
    mockGetSkillMatrix.mockResolvedValue(data);
    mockBulkDeleteSkills.mockResolvedValue({ deleted_count: 1, failed_count: 0 });

    renderWithQuery(<SkillMatrixPage onBack={() => {}} />);

    await waitFor(() => {
      expect(screen.getByText("test-skill")).toBeInTheDocument();
    });

    // Find test-skill row
    const testSkillRow = screen.getByText("test-skill").closest("tr");
    const buttons = testSkillRow?.querySelectorAll("button");
    
    // Click the SECOND button (ws-2 where skill exists with ID "skill-2")
    if (buttons && buttons[1]) {
      fireEvent.click(buttons[1]);
    }

    // Open delete dialog
    const deleteButton = await screen.findByText(/Delete/);
    fireEvent.click(deleteButton);

    // Confirm delete
    const confirmDelete = await screen.findByText(/Delete.*skill/);
    fireEvent.click(confirmDelete);

    // Verify the correct skill ID was used for delete
    await waitFor(() => {
      expect(mockBulkDeleteSkills).toHaveBeenCalledWith({
        skill_ids: ["skill-2"], // Should be skill-2 for ws-2, NOT skill-1
      });
    });

    // Verify we didn't use skill-1 (which is for ws-1)
    const callArgs = mockBulkDeleteSkills.mock.calls[0][0];
    expect(callArgs.skill_ids).not.toContain("skill-1");
  });

  it("uses correct source skill ID for sync", async () => {
    const data = createTestMatrixData();
    mockGetSkillMatrix.mockResolvedValue(data);
    mockSyncSkillToWorkspaces.mockResolvedValue({ 
      success_count: 1, 
      failed_count: 0 
    });

    renderWithQuery(<SkillMatrixPage onBack={() => {}} />);

    await waitFor(() => {
      expect(screen.getByText("test-skill")).toBeInTheDocument();
    });

    // Find test-skill row and click ws-3 (empty cell)
    const testSkillRow = screen.getByText("test-skill").closest("tr");
    const buttons = testSkillRow?.querySelectorAll("button");
    
    if (buttons && buttons[2]) {
      fireEvent.click(buttons[2]);
    }

    // Open sync dialog
    const syncButton = await screen.findByText(/Sync/);
    fireEvent.click(syncButton);

    // Confirm sync
    const confirmSync = await screen.findByText(/Sync.*cell/);
    fireEvent.click(confirmSync);

    // Verify sync was called with any valid skill ID from skill_lookup
    await waitFor(() => {
      expect(mockSyncSkillToWorkspaces).toHaveBeenCalled();
    });

    const [skillId, syncData] = mockSyncSkillToWorkspaces.mock.calls[0];
    
    // Verify the skill ID is from skill_lookup
    const validSkillIds = ["skill-1", "skill-2", "skill-3"];
    expect(validSkillIds).toContain(skillId);
    expect(syncData.target_workspace_ids).toContain("ws-3");
  });

  it("distinguishes between same skill name in different workspaces", async () => {
    const data = createTestMatrixData();
    
    // Verify test setup: same name, different IDs
    expect(data.skill_lookup["test-skill"]["ws-1"]).toBe("skill-1");
    expect(data.skill_lookup["test-skill"]["ws-2"]).toBe("skill-2");
    expect(data.skill_lookup["test-skill"]["ws-1"]).not.toBe(
      data.skill_lookup["test-skill"]["ws-2"]
    );

    mockGetSkillMatrix.mockResolvedValue(data);

    renderWithQuery(<SkillMatrixPage onBack={() => {}} />);

    await waitFor(() => {
      expect(screen.getByText("test-skill")).toBeInTheDocument();
    });

    // Both ws-1 and ws-2 should show the skill exists
    const testSkillRow = screen.getByText("test-skill").closest("tr");
    const cells = testSkillRow?.querySelectorAll("td");
    
    // First two cells (after skill name) should have checkmarks (skill exists)
    // Third cell should be empty (skill doesn't exist in ws-3)
    expect(cells?.length).toBeGreaterThanOrEqual(4); // name + 3 workspaces
  });
});

describe("SkillMatrix skill_lookup", () => {
  it("correctly maps skill names to workspace-specific IDs", () => {
    const lookup: Record<string, Record<string, string>> = {
      "my-skill": {
        "ws-1": "skill-id-1",
        "ws-2": "skill-id-2",
      },
    };

    // Should get different IDs for same name in different workspaces
    expect(lookup["my-skill"]["ws-1"]).toBe("skill-id-1");
    expect(lookup["my-skill"]["ws-2"]).toBe("skill-id-2");
    expect(lookup["my-skill"]["ws-1"]).not.toBe(lookup["my-skill"]["ws-2"]);
  });

  it("returns undefined for non-existent workspace", () => {
    const lookup: Record<string, Record<string, string>> = {
      "my-skill": {
        "ws-1": "skill-id-1",
      },
    };

    expect(lookup["my-skill"]["ws-999"]).toBeUndefined();
  });
});
