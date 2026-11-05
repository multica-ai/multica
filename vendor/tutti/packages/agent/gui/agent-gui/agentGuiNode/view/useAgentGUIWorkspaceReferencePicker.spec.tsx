import { act, renderHook } from "@testing-library/react";
import type { ReferenceSourceAggregator } from "@tutti-os/workspace-file-reference/core";
import { describe, expect, it } from "vitest";

import { useAgentGUIWorkspaceReferencePicker } from "./useAgentGUIWorkspaceReferencePicker";

function createInput(
  projectDirectorySourceAggregator: ReferenceSourceAggregator
): Parameters<typeof useAgentGUIWorkspaceReferencePicker>[0] {
  return {
    onWorkspaceFileReferencesAdded: undefined,
    projectDirectorySourceAggregator,
    referenceSourceAggregator: {} as ReferenceSourceAggregator,
    resolveMentionReferenceTarget: undefined,
    resolveWorkspaceReferenceInitialTarget: undefined,
    ...({
      composer: {
        state: { composerSettings: { selectedProjectPath: null } }
      },
      rail: {
        activeConversation: null,
        userProjects: []
      },
      shell: { workspaceId: "workspace-1" }
    } as unknown as Pick<
      Parameters<typeof useAgentGUIWorkspaceReferencePicker>[0],
      | "shell"
      | "rail"
      | "detail"
      | "composer"
      | "interaction"
      | "readiness"
      | "operations"
    >),
    workspaceFileReferenceAdapter: undefined,
    workspaceFileReferenceCopy: undefined
  };
}

describe("useAgentGUIWorkspaceReferencePicker project directory purpose", () => {
  it("allows Host folders for ordinary workspace references", () => {
    const { result } = renderHook(() =>
      useAgentGUIWorkspaceReferencePicker(
        createInput({} as ReferenceSourceAggregator)
      )
    );

    expect(result.current.workspaceReferencePickerPurpose).toBe("reference");
    expect(
      result.current.isWorkspaceReferencePickerNodeSelectable({
        displayName: "host-folder",
        kind: "folder",
        ref: { nodeId: "/Users/me/project", sourceId: "host-local-file" }
      })
    ).toBe(true);
  });

  it("opens the shared picker with the directory aggregator and resolves one folder", async () => {
    const projectDirectorySourceAggregator = {} as ReferenceSourceAggregator;
    const { result } = renderHook(() =>
      useAgentGUIWorkspaceReferencePicker(
        createInput(projectDirectorySourceAggregator)
      )
    );
    let selectionPromise!: Promise<{ path: string } | null>;

    act(() => {
      selectionPromise = result.current.requestProjectDirectory();
    });

    expect(result.current.workspaceReferencePickerOpen).toBe(true);
    expect(result.current.workspaceReferencePickerPurpose).toBe("directory");
    expect(result.current.workspaceReferencePickerAggregator).toBe(
      projectDirectorySourceAggregator
    );
    expect(
      result.current.isWorkspaceReferencePickerNodeSelectable({
        displayName: "folder",
        kind: "folder",
        ref: { nodeId: "/workspace/folder", sourceId: "workspace-file" }
      })
    ).toBe(true);
    expect(
      result.current.isWorkspaceReferencePickerNodeSelectable({
        displayName: "file",
        kind: "file",
        ref: { nodeId: "/workspace/file", sourceId: "workspace-file" }
      })
    ).toBe(false);

    act(() => {
      result.current.confirmWorkspaceReferencePicker([
        {
          kind: "folder",
          path: "/workspace/folder",
          sourceId: "workspace-file"
        }
      ]);
    });

    await expect(selectionPromise).resolves.toEqual({
      path: "/workspace/folder"
    });
    expect(result.current.workspaceReferencePickerOpen).toBe(false);
    expect(result.current.workspaceReferencePickerPurpose).toBe("reference");
  });
});
