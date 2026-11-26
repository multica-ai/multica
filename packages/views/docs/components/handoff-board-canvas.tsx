"use client";

import { useRef, useState } from "react";
import {
  CaptureUpdateAction,
  convertToExcalidrawElements,
  getSceneVersion,
  newElementWith,
  serializeAsJSON,
} from "@excalidraw/excalidraw";
import type { ExcalidrawElement } from "@excalidraw/excalidraw/element/types";
import type { ExcalidrawImperativeAPI, ExcalidrawInitialDataState } from "@excalidraw/excalidraw/types";
import { isDocRevisionConflict } from "@multica/core/docs";
import type { Issue } from "@multica/core/types";
import { ExcalidrawEditor } from "../../excalidraw/editor";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "../../i18n";
import { AppLink, useNavigation } from "../../navigation";
import { IssuePickerModal } from "../../modals/issue-picker-modal";
import type { HandoffBoardProps } from "./handoff-board";
import { boardIssuePath, boardMarkdown, linkedIssue } from "./handoff-board-model";

interface CanvasProps extends Omit<HandoffBoardProps, "markdown"> {
  initialData: ExcalidrawInitialDataState;
}

const excludeIds: string[] = [];

function sceneVersion(elements: readonly ExcalidrawElement[], background: string) {
  return `${getSceneVersion(elements)}:${background}`;
}

export default function HandoffBoardCanvas({
  initialData,
  workspaceId,
  workspaceSlug,
  onSave,
}: CanvasProps) {
  const { t } = useT("docs");
  const navigation = useNavigation();
  const [api, setApi] = useState<ExcalidrawImperativeAPI | null>(null);
  const [elements, setElements] = useState<readonly ExcalidrawElement[]>(initialData.elements ?? []);
  const [selected, setSelected] = useState<string | null>(null);
  const [pickingFor, setPickingFor] = useState<string | null>(null);
  const [status, setStatus] = useState<"idle" | "saving" | "saved" | "conflict" | "error">("idle");
  const lastSaved = useRef<string | null>(null);

  function insertHandoff() {
    if (!api || api.getSceneElements().length) return;
    const pair = convertToExcalidrawElements(
      [
        {
          type: "rectangle",
          id: "prepare",
          x: 40,
          y: 80,
          width: 190,
          height: 88,
          backgroundColor: "#d0ebff",
          fillStyle: "solid",
          label: { text: t(($) => $.board.prepare) },
        },
        {
          type: "rectangle",
          id: "review",
          x: 330,
          y: 80,
          width: 190,
          height: 88,
          backgroundColor: "#d3f9d8",
          fillStyle: "solid",
          label: { text: t(($) => $.board.review) },
        },
        {
          type: "arrow",
          x: 232,
          y: 124,
          points: [
            [0, 0],
            [96, 0],
          ],
          start: { id: "prepare" },
          end: { id: "review" },
        },
      ],
      { regenerateIds: false },
    );
    api.updateScene({
      elements: pair,
      appState: { selectedElementIds: { review: true } },
      captureUpdate: CaptureUpdateAction.IMMEDIATELY,
    });
    api.scrollToContent(pair, { fitToContent: true, maxZoom: 1 });
  }

  function attachIssue(issue: Issue) {
    if (!api || !pickingFor || issue.workspace_id !== workspaceId) return;
    const target = { workspace_id: workspaceId, id: issue.id, identifier: issue.identifier };
    api.updateScene({
      elements: api.getSceneElementsIncludingDeleted().map((element) =>
        element.id === pickingFor
          ? newElementWith(element, {
              customData: { ...element.customData, multicaIssue: target },
              link: boardIssuePath(workspaceSlug, target),
            })
          : element,
      ),
      captureUpdate: CaptureUpdateAction.IMMEDIATELY,
    });
  }

  async function save() {
    if (!api || status === "saving") return;
    setStatus("saving");
    try {
      const scene = serializeAsJSON(api.getSceneElements(), api.getAppState(), api.getFiles(), "local");
      const version = sceneVersion(
        api.getSceneElementsIncludingDeleted(),
        api.getAppState().viewBackgroundColor,
      );
      await onSave(boardMarkdown(scene));
      lastSaved.current = version;
      setStatus(
        version ===
          sceneVersion(api.getSceneElementsIncludingDeleted(), api.getAppState().viewBackgroundColor)
          ? "saved"
          : "idle",
      );
    } catch (error) {
      setStatus(isDocRevisionConflict(error) ? "conflict" : "error");
    }
  }

  const links = elements.flatMap((element) => {
    const issue = !element.isDeleted && linkedIssue(element.customData, workspaceId);
    if (!issue) return [];
    const label =
      element.type === "text"
        ? element
        : elements.find(
            (item) => !item.isDeleted && item.type === "text" && item.containerId === element.id,
          );
    return [{ key: element.id, issue, label: label?.type === "text" ? label.text : null }];
  });
  return (
    <div className="flex min-h-0 flex-1 flex-col gap-3 overflow-auto p-4">
      <div className="flex flex-wrap items-center gap-2">
        {!elements.some((element) => !element.isDeleted) && (
          <Button size="sm" variant="outline" disabled={!api} onClick={insertHandoff}>
            {t(($) => $.board.insert_handoff)}
          </Button>
        )}
        <Button size="sm" variant="outline" disabled={!selected} onClick={() => setPickingFor(selected)}>
          {t(($) => $.board.link_issue)}
        </Button>
        <Button size="sm" className="ml-auto" disabled={!api || status === "saving"} onClick={save}>
          {status === "saving" ? t(($) => $.editor.saving) : t(($) => $.board.save)}
        </Button>
        {status === "saved" && (
          <span role="status" className="text-caption text-muted-foreground">
            {t(($) => $.editor.saved)}
          </span>
        )}
        {status === "conflict" && (
          <span role="alert" className="text-caption text-destructive">
            {t(($) => $.board.conflict)}
          </span>
        )}
        {status === "error" && (
          <span role="alert" className="text-caption text-destructive">
            {t(($) => $.errors.save)}
          </span>
        )}
      </div>
      <div className="min-h-96 flex-1 overflow-hidden rounded-lg border">
        <ExcalidrawEditor
          initialData={initialData}
          excalidrawAPI={setApi}
          onChange={(next, appState) => {
            if (
              status === "saved" &&
              lastSaved.current !== sceneVersion(next, appState.viewBackgroundColor)
            )
              setStatus("idle");
            setElements(next);
            const ids = Object.keys(appState.selectedElementIds);
            setSelected(ids.length === 1 ? (ids[0] ?? null) : null);
          }}
          onLinkOpen={(element, event) => {
            event.preventDefault();
            const issue = linkedIssue(element.customData, workspaceId);
            if (issue) navigation.push(boardIssuePath(workspaceSlug, issue));
          }}
          validateEmbeddable={false}
          aiEnabled={false}
          UIOptions={{
            canvasActions: { loadScene: false, saveToActiveFile: false, saveAsImage: false },
            tools: { image: false },
          }}
        />
      </div>
      {links.length > 0 && (
        <ul className="flex flex-wrap gap-x-5 gap-y-1 text-label">
          {links.map(({ key, issue, label }) => (
            <li key={key} className="flex items-center gap-2">
              {label && <span className="text-muted-foreground">{label}</span>}
              <AppLink
                className="text-primary underline underline-offset-4"
                href={boardIssuePath(workspaceSlug, issue)}
              >
                {issue.identifier}
              </AppLink>
            </li>
          ))}
        </ul>
      )}
      {pickingFor && (
        <IssuePickerModal
          open
          title={t(($) => $.board.link_issue)}
          description={t(($) => $.board.pick_issue)}
          excludeIds={excludeIds}
          isSelectable={(issue) => issue.workspace_id === workspaceId}
          onSelect={attachIssue}
          onOpenChange={(open) => {
            if (!open) setPickingFor(null);
          }}
        />
      )}
    </div>
  );
}
