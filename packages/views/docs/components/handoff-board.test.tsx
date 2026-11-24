import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ComponentProps } from "react";
import { describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import type { ExcalidrawProps } from "@excalidraw/excalidraw/types";
import { HandoffBoard } from "./handoff-board";
import { boardMarkdown, readBoard } from "./handoff-board-model";
import enDocs from "../../locales/en/docs.json";

const push = vi.hoisted(() => vi.fn());
vi.mock("../../navigation", () => ({
  useNavigation: () => ({ push }),
  AppLink: (props: ComponentProps<"a">) => <a {...props} />,
}));
vi.mock("@excalidraw/excalidraw", async () => {
  const { useEffect, useState } = await import("react");
  return {
    getSceneVersion: () => 1,
    serializeAsJSON: (elements: unknown) => JSON.stringify({ elements }),
    Excalidraw: ({ initialData, excalidrawAPI, onLinkOpen }: ExcalidrawProps) => {
      const [scene] = useState(() => {
        const initial = initialData && "elements" in initialData ? initialData.elements : [];
        return initial ?? [];
      });
      useEffect(() => {
        excalidrawAPI?.({
          getSceneElements: () => scene,
          getSceneElementsIncludingDeleted: () => scene,
          getFiles: () => ({}),
          getAppState: () => ({ viewBackgroundColor: "#fff" }),
        } as Parameters<NonNullable<ExcalidrawProps["excalidrawAPI"]>>[0]);
      }, [scene, excalidrawAPI]);
      return (
        <button
          onClick={() => {
            const element = scene[0];
            if (element && !element.isDeleted)
              onLinkOpen?.(
                { ...element, isDeleted: false },
                new CustomEvent("link", {
                  detail: { nativeEvent: new MouseEvent("click") },
                  cancelable: true,
                }),
              );
          }}
        >
          Open shape link
        </button>
      );
    },
  };
});

const step = {
  id: "step",
  type: "rectangle",
  link: "/somewhere/else",
  customData: {
    multicaIssue: { id: "issue-12", workspace_id: "ws-a", identifier: "OPS-12" },
  },
};
const stored = boardMarkdown(JSON.stringify({ elements: [step] }));

function open(markdown: string, workspaceId: string, save: (value: string) => Promise<void>) {
  return render(
    <I18nProvider locale="en" resources={{ en: { docs: enDocs } }}>
      <HandoffBoard
        markdown={markdown}
        workspaceId={workspaceId}
        workspaceSlug="release"
        onSave={save}
      />
    </I18nProvider>,
  );
}

describe("handoff board", () => {
  it("saves the current scene, reopens it, and routes a shape click to its issue", async () => {
    let content = "";
    const first = open(stored, "ws-a", async (value) => {
      content = value;
    });
    const save = await screen.findByRole("button", { name: "Save board" });
    await waitFor(() => expect(save).toBeEnabled());
    fireEvent.click(save);
    await screen.findByText("Saved");
    expect(readBoard(content)?.elements).toEqual([step]);
    first.unmount();

    open(content, "ws-a", vi.fn());
    fireEvent.click(await screen.findByRole("button", { name: "Open shape link" }));
    expect(push).toHaveBeenCalledWith("/release/issues/OPS-12");
  });

  it("does not offer or follow another workspace's step link", async () => {
    push.mockClear();
    open(stored, "ws-b", vi.fn());
    fireEvent.click(await screen.findByRole("button", { name: "Open shape link" }));
    expect(screen.queryByRole("link")).not.toBeInTheDocument();
    expect(push).not.toHaveBeenCalled();
  });
});
