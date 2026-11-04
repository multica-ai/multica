import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { chatKeys, chatPinnedAgentsOptions } from "../chat/queries";
import { createLogger } from "../logger";
import { autopilotKeys } from "../autopilots/queries";
import { workspaceKeys } from "../workspace/queries";
import type { ChatPinnedAgent } from "../types";
import { companyKeys } from "./queries";
import { companySetupKeys } from "./setup-query";
import type { CompanySetupReadiness, CompanySetupRequest } from "./setup-schema";

const logger = createLogger("company.setup");

/**
 * Pins the Secretary in the viewer's chat, once. A pin already in the viewer's
 * list is left alone, so running setup again does not repeat it. A failed pin
 * is logged, not thrown: the setup it follows has already happened.
 */
async function pinSecretaryOnce(
  qc: ReturnType<typeof useQueryClient>,
  wsId: string,
  agentId: string,
): Promise<void> {
  let pinned: ChatPinnedAgent[] = [];
  try {
    pinned = await qc.fetchQuery(chatPinnedAgentsOptions(wsId));
  } catch (err) {
    // Without the list, pin anyway: the server treats a repeat pin as a no-op.
    logger.warn("pinned list unavailable", { err });
  }
  if (pinned.some((entry) => entry.agent_id === agentId)) return;
  try {
    await api.pinChatAgent(agentId);
    qc.invalidateQueries({ queryKey: chatKeys.pinnedAgents(wsId) });
  } catch (err) {
    logger.warn("pin secretary failed", { agentId, err });
  }
}

/**
 * Runs `POST /api/company/setup` and, once the Secretary exists, pins it in the
 * viewer's chat. Awaits the server: a refusal (409, see parseCompanySetupRefusal)
 * is the caller's to show, and nothing is patched optimistically.
 */
export function useSetupCompany(wsId: string) {
  const qc = useQueryClient();
  return useMutation<CompanySetupReadiness, Error, CompanySetupRequest>({
    mutationFn: async (data) => {
      const readiness = await api.setupCompany(data);
      if (readiness.secretary_id) await pinSecretaryOnce(qc, wsId, readiness.secretary_id);
      return readiness;
    },
    onSuccess: (readiness) => {
      qc.setQueryData(companySetupKeys.all(wsId), readiness);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: companySetupKeys.all(wsId) });
      qc.invalidateQueries({ queryKey: companyKeys.floors(wsId) });
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
      qc.invalidateQueries({ queryKey: workspaceKeys.squads(wsId) });
      qc.invalidateQueries({ queryKey: autopilotKeys.list(wsId) });
    },
  });
}
