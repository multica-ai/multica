import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import {
  useCallback,
  useImperativeHandle,
  useMemo,
  useRef,
  type JSX,
  type ReactNode,
  type Ref,
  type RefObject
} from "react";
import {
  findMessageLocatorScrollParent,
  scrollTranscriptRowIntoView
} from "./AgentMessageLocatorRail";
import type { AgentTranscriptTurnGroup } from "./agentTranscriptModel";
import { observer } from "mobx-react-lite";
export const AgentTranscriptAttachmentView = observer(
  AgentTranscriptAttachmentViewRender
);

export interface AgentTranscriptTurnAttachment {
  id: string;
  anchorTurnId: string | null;
  /**
   * Anchored timeline chrome should stay absent until its Turn page is loaded.
   * Existing workflow attachments retain the historical trailing fallback.
   */
  missingAnchorBehavior?: "trailing" | "hide";
  content: ReactNode;
}

export type AgentTranscriptAttachmentLocator = (attachmentId: string) => void;

interface TurnAttachmentVirtualizer {
  scrollToIndex(index: number, options: { align: "center" }): void;
}

export function useAgentTranscriptTurnAttachments(
  input: useAgentTranscriptTurnAttachmentsProps
): {
  byGroupIndex: ReadonlyMap<number, readonly AgentTranscriptTurnAttachment[]>;
  onElementChange: (attachmentId: string, element: HTMLElement | null) => void;
  trailing: readonly AgentTranscriptTurnAttachment[];
} {
  const projection = useMemo(() => {
    const lastGroupIndexByTurnId = new Map<string, number>();
    input.turnGroups.forEach((group, groupIndex) => {
      if (group.turnId) lastGroupIndexByTurnId.set(group.turnId, groupIndex);
    });
    const byGroupIndex = new Map<number, AgentTranscriptTurnAttachment[]>();
    const trailing: AgentTranscriptTurnAttachment[] = [];
    for (const attachment of input.attachments) {
      const groupIndex = attachment.anchorTurnId
        ? lastGroupIndexByTurnId.get(attachment.anchorTurnId)
        : undefined;
      if (groupIndex === undefined) {
        if (attachment.missingAnchorBehavior !== "hide") {
          trailing.push(attachment);
        }
        continue;
      }
      const groupAttachments = byGroupIndex.get(groupIndex) ?? [];
      groupAttachments.push(attachment);
      byGroupIndex.set(groupIndex, groupAttachments);
    }
    const groupIndexByAttachmentId = new Map<string, number>();
    byGroupIndex.forEach((attachments, groupIndex) => {
      attachments.forEach((attachment) =>
        groupIndexByAttachmentId.set(attachment.id, groupIndex)
      );
    });
    return { byGroupIndex, groupIndexByAttachmentId, trailing };
  }, [input.attachments, input.turnGroups]);
  const attachmentElementsRef = useRef(new Map<string, HTMLElement>());
  const attachmentObserverRef = useRef<IntersectionObserver | null>(null);

  const onElementChange = useCallback(
    (attachmentId: string, element: HTMLElement | null): void => {
      const previous = attachmentElementsRef.current.get(attachmentId);
      if (previous && previous !== element) {
        attachmentObserverRef.current?.unobserve(previous);
      }
      if (!element) {
        attachmentElementsRef.current.delete(attachmentId);
        input.onVisibilityChange?.(attachmentId, false);
        if (attachmentElementsRef.current.size === 0) {
          attachmentObserverRef.current?.disconnect();
          attachmentObserverRef.current = null;
        }
        return;
      }
      attachmentElementsRef.current.set(attachmentId, element);
      if (
        !attachmentObserverRef.current &&
        input.onVisibilityChange &&
        typeof IntersectionObserver === "function"
      ) {
        attachmentObserverRef.current = new IntersectionObserver((entries) => {
          for (const entry of entries) {
            const id = (entry.target as HTMLElement).dataset
              .agentTranscriptAttachment;
            if (id) input.onVisibilityChange?.(id, entry.isIntersecting);
          }
        });
      }
      attachmentObserverRef.current?.observe(element);
    },
    [input.onVisibilityChange]
  );

  const locateAttachment = useCallback(
    (attachmentId: string): void => {
      const scrollParent = input.virtualizerHostRef.current
        ? findMessageLocatorScrollParent(input.virtualizerHostRef.current)
        : null;
      const scrollToRenderedAttachment = (): boolean => {
        const renderedAttachment =
          attachmentElementsRef.current.get(attachmentId);
        if (!renderedAttachment) return false;
        scrollTranscriptRowIntoView(
          renderedAttachment,
          scrollParent ?? findMessageLocatorScrollParent(renderedAttachment)
        );
        renderedAttachment.animate?.(
          [
            { boxShadow: "0 0 0 2px var(--tutti-purple)" },
            { boxShadow: "0 0 0 2px transparent" }
          ],
          { duration: 1400, easing: "ease-out" }
        );
        return true;
      };

      if (scrollToRenderedAttachment()) return;
      const groupIndex = projection.groupIndexByAttachmentId.get(attachmentId);
      if (input.shouldVirtualize && groupIndex !== undefined) {
        input.rowVirtualizer.scrollToIndex(groupIndex, { align: "center" });
        requestAnimationFrame(scrollToRenderedAttachment);
      }
    },
    [
      input.rowVirtualizer,
      input.shouldVirtualize,
      input.virtualizerHostRef,
      projection.groupIndexByAttachmentId
    ]
  );
  useImperativeHandle(input.locatorRef ?? null, () => locateAttachment, [
    locateAttachment
  ]);

  return {
    byGroupIndex: projection.byGroupIndex,
    onElementChange,
    trailing: projection.trailing
  };
}

function AgentTranscriptAttachmentViewRender({
  attachment,
  onElementChange
}: {
  attachment: AgentTranscriptTurnAttachment;
  onElementChange: (attachmentId: string, element: HTMLElement | null) => void;
}): JSX.Element {
  const handleRef = useCallback(
    (element: HTMLDivElement | null) => onElementChange(attachment.id, element),
    [attachment.id, onElementChange]
  );
  const handleRefCallback = usePersistCallback(handleRef);
  return (
    <div
      ref={handleRefCallback}
      className="agent-gui-transcript-attachment"
      data-agent-transcript-attachment={attachment.id}
    >
      {attachment.content}
    </div>
  );
}

type useAgentTranscriptTurnAttachmentsProps = {
  attachments: readonly AgentTranscriptTurnAttachment[];
  locatorRef?: Ref<AgentTranscriptAttachmentLocator>;
  onVisibilityChange?: (attachmentId: string, visible: boolean) => void;
  rowVirtualizer: TurnAttachmentVirtualizer;
  shouldVirtualize: boolean;
  turnGroups: readonly Pick<AgentTranscriptTurnGroup, "key" | "turnId">[];
  virtualizerHostRef: RefObject<HTMLDivElement | null>;
};
