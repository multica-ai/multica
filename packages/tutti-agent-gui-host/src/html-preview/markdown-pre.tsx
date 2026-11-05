import { type ComponentProps } from "react";
import { MarkdownPre as OriginalMarkdownPre } from "../../../../vendor/tutti/packages/agent/gui/shared/AgentMessageMarkdownRenderers";
import { DocumentPreview } from "./preview";
import { codeBlockPreview } from "./code-block";
import { observer } from "mobx-react-lite";
export const MarkdownPre = observer(MarkdownPreRender);

function MarkdownPreRender(props: ComponentProps<typeof OriginalMarkdownPre>) {
  const preview =
    props.mermaidEnabled !== false ? codeBlockPreview(props.children) : null;
  return preview === null ? (
    <OriginalMarkdownPre {...props} />
  ) : (
    <DocumentPreview {...preview} streaming={props.streaming ?? false} />
  );
}
