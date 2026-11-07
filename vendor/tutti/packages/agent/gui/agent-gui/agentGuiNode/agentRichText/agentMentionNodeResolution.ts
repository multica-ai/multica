import { parseRichTextMentionHref } from "@tutti-os/ui-rich-text/core";
import type { RichTextMentionResolutionState } from "@tutti-os/ui-rich-text/service";
import type {
  RichTextMentionIdentity,
  RichTextMentionResolved
} from "@tutti-os/ui-rich-text/types";

export interface AgentMentionResolvedPresentation {
  agentProviderId?: string;
  iconUrl?: string;
  label: string;
}

type ResolvableAgentMentionKind =
  | "agent-target"
  | "workspace-member"
  | "workspace-app"
  | "workspace-issue";

export function parseResolvableAgentMentionIdentity(
  attrs: Record<string, unknown>,
  kind: string
): RichTextMentionIdentity | null {
  if (!isResolvableAgentMentionKind(kind)) {
    return null;
  }
  return parseRichTextMentionHref(
    attrString(attrs, "href"),
    attrString(attrs, "name")
  );
}

export function resolveAgentMentionNodePresentation(input: {
  attrs: Record<string, unknown>;
  hasMentionService: boolean;
  resolved?: RichTextMentionResolved;
  state: RichTextMentionResolutionState;
}): AgentMentionResolvedPresentation {
  const canonicalLabel = attrString(input.attrs, "name").trim();
  if (
    !input.hasMentionService ||
    input.state === "missing" ||
    input.state === "error"
  ) {
    return { label: canonicalLabel };
  }
  if (input.state !== "ready") {
    return {
      agentProviderId:
        attrString(input.attrs, "agentProviderId").trim() || undefined,
      iconUrl: attrString(input.attrs, "iconUrl").trim() || undefined,
      label: canonicalLabel
    };
  }
  const presentation = input.resolved?.presentation;
  return {
    agentProviderId: presentation?.agentProviderId?.trim() || undefined,
    iconUrl:
      presentation?.iconUrl?.trim() ||
      presentation?.thumbnailUrl?.trim() ||
      presentation?.agentIconUrl?.trim() ||
      undefined,
    label:
      input.resolved?.label?.trim().replace(/^@+/, "").trim() || canonicalLabel
  };
}

function isResolvableAgentMentionKind(
  kind: string
): kind is ResolvableAgentMentionKind {
  return (
    kind === "agent-target" ||
    kind === "workspace-member" ||
    kind === "workspace-app" ||
    kind === "workspace-issue"
  );
}

function attrString(attrs: Record<string, unknown>, key: string): string {
  const value = attrs[key];
  return typeof value === "string" ? value : "";
}
