import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { autopilotKeys } from "../autopilots/queries";
import { issueWorkflowKeys } from "../issue-workflows/queries";
import type { CompanyStart, CompanySquadKey } from "../squads/company-template";
import { workspaceKeys } from "../workspace/queries";
import { officeKeys } from "./queries";
import { officeSetupKeys } from "./setup-query";
import type {
  CompanyTemplateApplyRequest,
  CompanyTemplateApplyResult,
} from "./company-template-types";

/**
 * The request the wizard sends. A blank model is left out, so the runtime's
 * default applies rather than an empty string.
 */
export function buildCompanyTemplateApplyRequest(input: {
  runtimeId: string;
  model: string;
  squads: readonly CompanySquadKey[];
  start: CompanyStart;
}): CompanyTemplateApplyRequest {
  const model = input.model.trim();
  return {
    runtime_id: input.runtimeId,
    ...(model ? { model } : {}),
    squads: [...input.squads],
    start: input.start,
  };
}

/**
 * Runs the starter company's apply. The server answers with each step, so the
 * caller reports a partial result instead of treating it as an error. Nothing
 * is patched optimistically: the caches the apply touches are refetched once
 * it settles.
 */
export function useApplyCompanyTemplate(wsId: string) {
  const qc = useQueryClient();
  return useMutation<CompanyTemplateApplyResult, Error, CompanyTemplateApplyRequest>({
    mutationFn: (data) => api.applyCompanyTemplate(data),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
      qc.invalidateQueries({ queryKey: workspaceKeys.squads(wsId) });
      qc.invalidateQueries({ queryKey: officeKeys.floors(wsId) });
      qc.invalidateQueries({ queryKey: officeSetupKeys.all(wsId) });
      qc.invalidateQueries({ queryKey: autopilotKeys.list(wsId) });
      qc.invalidateQueries({ queryKey: issueWorkflowKeys.all(wsId) });
    },
  });
}
