import { useMemo, type ReactNode } from "react";
import {
  RichTextMentionServiceProvider,
  useRichTextMentionService
} from "@tutti-os/ui-rich-text/editor";
import type { RichTextMentionService } from "@tutti-os/ui-rich-text/service";
import type { AgentGUIObservationGapSource } from "../../types";
import { AgentObservationGapSourceProvider } from "../../shared/agentConversation/AgentObservationGapContext";
import { composeAgentGUIMentionService } from "./composeAgentGUIMentionService";
import { observer } from "mobx-react-lite";
export const AgentGUIMentionServiceBoundary = observer(
  AgentGUIMentionServiceBoundaryRender
);

function AgentGUIMentionServiceBoundaryRender({
  children,
  observationGapSource,
  service
}: {
  children: ReactNode;
  observationGapSource?: AgentGUIObservationGapSource | null;
  service?: RichTextMentionService;
}): ReactNode {
  const inheritedService = useRichTextMentionService();
  const effectiveService = useMemo(
    () =>
      service && inheritedService && service !== inheritedService
        ? composeAgentGUIMentionService({
            inheritedService,
            surfaceService: service
          })
        : service,
    [inheritedService, service]
  );

  const content =
    !effectiveService || effectiveService === inheritedService ? (
      children
    ) : (
      <RichTextMentionServiceProvider service={effectiveService}>
        {children}
      </RichTextMentionServiceProvider>
    );

  return (
    <AgentObservationGapSourceProvider source={observationGapSource}>
      {content}
    </AgentObservationGapSourceProvider>
  );
}
