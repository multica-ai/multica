import React from "react";
import { vi } from "vitest";
import { render, type RenderOptions } from "@testing-library/react";
import type { User, Workspace, MemberWithUser, Agent } from "@/shared/types";

// Mock user
export const mockUser: User = {
  id: "user-1",
  name: "Test User",
  email: "test@multica.ai",
  avatar_url: null,
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
};

// Mock workspace
export const mockWorkspace: Workspace = {
  id: "ws-1",
  name: "Test Workspace",
  slug: "test-ws",
  description: "A test workspace",
  context: null,
  settings: {},
  repos: [],
  issue_prefix: "TES",
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
};

// Mock members
export const mockMembers: MemberWithUser[] = [
  {
    id: "member-1",
    workspace_id: "ws-1",
    user_id: "user-1",
    role: "owner",
    created_at: "2026-01-01T00:00:00Z",
    name: "Test User",
    email: "test@multica.ai",
    avatar_url: null,
  },
];

// Mock agents
export const mockAgents: Agent[] = [
  {
    id: "agent-1",
    workspace_id: "ws-1",
    runtime_id: "runtime-1",
    name: "Claude Agent",
    description: "",
    instructions: "",
    avatar_url: null,
    status: "idle",
    runtime_mode: "cloud",
    runtime_config: {},
    visibility: "workspace",
    max_concurrent_tasks: 3,
    owner_id: null,
    skills: [],
    tools: [],
    triggers: [],
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    archived_at: null,
    archived_by: null,
  },
];

// Mock auth context value
// eslint-disable-next-line @typescript-eslint/no-explicit-any
export const mockAuthValue: Record<string, any> = {
  user: mockUser,
  workspace: mockWorkspace,
  members: mockMembers,
  agents: mockAgents,
  isLoading: false,
  login: vi.fn(),
  logout: vi.fn(),
  workspaces: [mockWorkspace],
  switchWorkspace: vi.fn(),
  createWorkspace: vi.fn(),
  updateWorkspace: vi.fn(),
  updateCurrentUser: vi.fn(),
  leaveWorkspace: vi.fn(),
  deleteWorkspace: vi.fn(),
  refreshWorkspaces: vi.fn(),
  refreshMembers: vi.fn(),
  refreshAgents: vi.fn(),
  getMemberName: (userId: string) => {
    const m = mockMembers.find((m) => m.user_id === userId);
    return m?.name ?? "Unknown";
  },
  getAgentName: (agentId: string) => {
    const a = mockAgents.find((a) => a.id === agentId);
    return a?.name ?? "Unknown Agent";
  },
  getActorName: (type: string, id: string) => {
    if (type === "member") {
      const m = mockMembers.find((m) => m.user_id === id);
      return m?.name ?? "Unknown";
    }
    if (type === "agent") {
      const a = mockAgents.find((a) => a.id === id);
      return a?.name ?? "Unknown Agent";
    }
    return "System";
  },
  getActorInitials: (type: string, id: string) => {
    return "TU";
  },
};
