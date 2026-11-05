import { useMemo } from "react";
import { useConversationServices } from "../../../conversationServices";
import { useTranslation } from "../../../i18n";
import { EditRetryPresentationState } from "../../../state/EditRetryPresentationState";

export function useAgentGUIEditRetryController(input: {
  agentSessionId: string | null;
  workspaceId: string;
}) {
  const { prompts } = useConversationServices();
  const { t } = useTranslation();
  return useMemo(
    () =>
      new EditRetryPresentationState(prompts, input.agentSessionId, {
        edit: t("agentHost.agentGui.editRetryEditMessage"),
        cancel: t("agentHost.agentGui.editRetryCancel"),
        submit: t("agentHost.agentGui.send")
      }),
    [prompts, input.agentSessionId, t]
  );
}
