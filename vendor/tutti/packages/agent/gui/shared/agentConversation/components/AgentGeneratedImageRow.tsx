import { observer } from "mobx-react-lite";
import type { JSX } from "react";
import { translate } from "../../../i18n/index";
import type { AgentGeneratedImageRowVM } from "../contracts/agentGeneratedImageRowVM";
import { AgentGeneratedImagePreview } from "./AgentGeneratedImagePreview";

export const AgentGeneratedImageRow = observer(function AgentGeneratedImageRow({
  row
}: {
  row: AgentGeneratedImageRowVM;
}): JSX.Element {
  "use memo";
  return (
    <div
      className="flex max-w-full justify-start"
      data-testid="agent-generated-image-artifact"
    >
      <AgentGeneratedImagePreview
        uri={row.uri}
        mimeType={row.mimeType}
        alt={translate("agentHost.agentTool.details.imagePreviewAlt")}
        className="block max-h-[560px] max-w-full rounded-[10px] border border-[var(--line-2)] bg-[var(--background-panel)] object-contain"
      />
    </div>
  );
});
