import type { JSX } from "react";
import type { AgentApprovalItemVM } from "../contracts/agentApprovalItemVM";
import { translate } from "../../../i18n/index";
import {
  Spinner,
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger
} from "@tutti-os/ui-system";
import { normalizeApprovalOptionToken } from "../approvalOptionPresentation";
import {
  getPromptToolDetails,
  type PromptToolDetail
} from "../promptToolDetails";
import styles from "../../../agent-gui/agentGuiNode/AgentGUIConversation.styles";
import { observer } from "mobx-react-lite";
export const InteractiveOptionSpinner = observer(
  InteractiveOptionSpinnerRender
);
export const SendFilledIcon = observer(SendFilledIconRender);
export const PromptDetailValue = observer(PromptDetailValueRender);
export const CommandTextWithTooltip = observer(CommandTextWithTooltipRender);

const COMMAND_TOOLTIP_DELAY_MS = 1000;

export function isEnterLikeKey(event: KeyboardEvent): boolean {
  return (
    event.key === "Enter" ||
    event.code === "Enter" ||
    event.code === "NumpadEnter"
  );
}

export function isEditableKeyboardTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) {
    return false;
  }
  const tagName = target.tagName.toLowerCase();
  return (
    tagName === "input" ||
    tagName === "textarea" ||
    tagName === "select" ||
    target.isContentEditable
  );
}

export function approvalOptionShortcutLabel(
  optionIndex: number,
  isDarwin: boolean
): string | null {
  if (optionIndex === 0) {
    return translate("agentHost.agentGui.shortcutEnter");
  }
  if (optionIndex === 1) {
    return isDarwin
      ? translate("agentHost.agentGui.shortcutCmdEnter")
      : translate("agentHost.agentGui.shortcutCtrEnter");
  }
  return null;
}

export function isDarwinPlatform(platform: string | undefined): boolean {
  if (platform) {
    return platform === "darwin";
  }
  if (typeof navigator === "undefined") {
    return false;
  }
  const userAgentPlatform =
    "userAgentData" in navigator
      ? (
          navigator as Navigator & {
            userAgentData?: { platform?: string };
          }
        ).userAgentData?.platform
      : undefined;
  const navigatorPlatform = userAgentPlatform ?? navigator.platform ?? "";
  return /mac/i.test(navigatorPlatform);
}

function InteractiveOptionSpinnerRender(): JSX.Element {
  "use memo";
  return (
    <Spinner
      className={styles.interactiveOptionSpinner}
      testId="agent-interactive-option-spinner"
    />
  );
}

function SendFilledIconRender(): JSX.Element {
  "use memo";
  return (
    <svg
      width="24"
      height="24"
      viewBox="0 0 24 24"
      fill="none"
      aria-hidden="true"
    >
      <path
        d="M2.74311 8.80587C2.84592 8.40096 3.14571 8.08844 3.54551 7.97033L18.5197 3.51569C18.9336 3.39383 19.3809 3.5054 19.6881 3.81262C19.9951 4.11984 20.1076 4.56798 19.9857 4.9817L15.5311 19.9559C15.413 20.3557 15.1005 20.6555 14.6956 20.7583C14.2895 20.8597 13.869 20.7438 13.5721 20.4469L10.455 15.1823C10.8585 14.6483 12.1563 12.9094 14.3475 9.96528C14.6086 9.70419 14.6382 9.31168 14.4138 9.08692C14.1891 8.86221 13.796 8.8913 13.5348 9.15252L8.31088 13.0423L3.05316 9.92799C2.7562 9.63104 2.64049 9.21071 2.74311 8.80587Z"
        fill="currentColor"
      />
    </svg>
  );
}

export function interactivePromptClassName(embedded: boolean): string {
  return embedded
    ? `${styles.interactivePrompt} agent-gui-conversation__interactive-prompt--embedded`
    : styles.interactivePrompt;
}

export function interactivePromptCardClassName(edgeGlow: boolean): string {
  return edgeGlow
    ? `${styles.interactivePromptCard} agent-gui-edge-glow`
    : styles.interactivePromptCard;
}

export interface LabeledPromptToolDetail {
  kind: PromptToolDetail["kind"];
  label: string;
  value: string;
  meta?: string;
}

export interface ApprovalPromptToolPresentation {
  lead: string;
  leadDetails: LabeledPromptToolDetail[];
  cardDetails: LabeledPromptToolDetail[];
}

export function formatToolDetails(
  input: Record<string, unknown> | null
): LabeledPromptToolDetail[] {
  return getPromptToolDetails(input).map((detail) => ({
    kind: detail.kind,
    label: promptToolDetailLabel(detail.kind),
    value: detail.value,
    ...(detail.meta ? { meta: detail.meta } : {})
  }));
}

export function formatApprovalToolPresentation(
  prompt: Pick<AgentApprovalItemVM, "approvalPurpose" | "input">,
  labels: { approvalLead: string; fileChangeApprovalLead: string }
): ApprovalPromptToolPresentation {
  const details = formatToolDetails(prompt.input);
  const hasFileChanges = details.some((detail) => detail.kind === "files");
  const isLeadDetail = (detail: LabeledPromptToolDetail): boolean =>
    detail.kind === "reason" ||
    (hasFileChanges && (detail.kind === "directory" || detail.kind === "path"));
  const leadDetails = [
    ...details.filter((detail) => detail.kind === "reason"),
    ...details.filter(
      (detail) => detail.kind !== "reason" && isLeadDetail(detail)
    )
  ].filter(
    (detail, index, candidates) =>
      candidates.findIndex(
        (candidate) =>
          candidate.value === detail.value && candidate.meta === detail.meta
      ) === index
  );
  const purposeLeads = {
    generic: labels.approvalLead,
    "edit-files": labels.fileChangeApprovalLead
  } satisfies Record<
    NonNullable<AgentApprovalItemVM["approvalPurpose"]> | "generic",
    string
  >;
  return {
    lead: purposeLeads[prompt.approvalPurpose ?? "generic"],
    leadDetails,
    cardDetails: details.filter((detail) => !isLeadDetail(detail))
  };
}

function PromptDetailValueRender({
  detail
}: {
  detail: LabeledPromptToolDetail;
}): JSX.Element {
  "use memo";
  if (detail.kind !== "command") {
    return (
      <span className={styles.interactiveOptionDescription}>
        {detail.value}
      </span>
    );
  }
  return (
    <CommandTextWithTooltip
      value={detail.value}
      testId="agent-interactive-command-detail"
      tooltipsEnabled
    />
  );
}

function CommandTextWithTooltipRender({
  value,
  testId,
  tooltipsEnabled = true
}: {
  value: string;
  testId: string;
  tooltipsEnabled?: boolean;
}): JSX.Element {
  "use memo";
  const content = (
    <span
      className={`${styles.interactiveOptionDescription} ${styles.interactiveOptionCommandDescription}`}
      data-agent-interactive-command-detail={
        testId === "agent-interactive-command-detail" ? "true" : undefined
      }
      data-agent-interactive-command-prefix-option={
        testId === "agent-interactive-command-prefix-option"
          ? "true"
          : undefined
      }
    >
      {value}
    </span>
  );

  if (!tooltipsEnabled) {
    return content;
  }

  return (
    <TooltipProvider delayDuration={COMMAND_TOOLTIP_DELAY_MS}>
      <Tooltip>
        <TooltipTrigger asChild>{content}</TooltipTrigger>
        <TooltipContent className={styles.interactiveOptionCommandTooltip}>
          {value}
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
}

export function promptToolDetailLabel(kind: PromptToolDetail["kind"]): string {
  switch (kind) {
    case "command":
      return translate("agentHost.agentTool.details.command");
    case "directory":
      return translate("agentHost.agentTool.details.scope");
    case "files":
      return translate("workspaceCanvas.nodeDockLabel.files");
    case "mcp":
      return translate("agentHost.agentTool.details.mcp");
    case "path":
      return translate("agentHost.agentTool.details.path");
    case "prompt":
      return translate("agentHost.agentTool.details.prompt");
    case "query":
      return translate("agentHost.agentTool.details.query");
    case "reason":
      return translate("agentHost.agentTool.details.summary");
    case "url":
      return translate("agentHost.agentTool.details.url");
  }
}

export function isApprovalFeedbackOption(option: {
  id: string;
  kind: string;
}): boolean {
  return (
    isDenyApprovalOptionToken(option.id) ||
    isDenyApprovalOptionToken(option.kind)
  );
}

export function approvalFeedbackOptionId(
  options: readonly { id: string; kind: string }[]
): string | null {
  const explicitFeedbackOption = options.find((option) =>
    isExplicitFeedbackDenyApprovalOption(option)
  );
  if (explicitFeedbackOption) {
    return explicitFeedbackOption.id;
  }
  return options.find(isApprovalFeedbackOption)?.id ?? null;
}

export function isExplicitFeedbackDenyApprovalOption(option: {
  id: string;
  kind: string;
}): boolean {
  for (const value of [option.id, option.kind]) {
    switch (normalizeApprovalOptionToken(value ?? "")) {
      case "denywithfeedback":
      case "rejectwithfeedback":
        return true;
      default:
        break;
    }
  }
  return false;
}

export function isDenyApprovalOptionToken(
  value: string | null | undefined
): boolean {
  switch (normalizeApprovalOptionToken(value ?? "")) {
    case "abort":
    case "cancel":
    case "cancelled":
    case "canceled":
    case "deny":
    case "denied":
    case "reject":
    case "rejected":
    case "rejectonce":
    case "disallow":
    case "decline":
    case "declined":
    case "no":
      return true;
    default:
      return false;
  }
}

export function stripPromptTitlePunctuation(value: string): string {
  return value.trim().replace(/[.。]+$/u, "");
}

export function interactiveOptionLabel(
  label: string,
  description: string | null | undefined
): string {
  const trimmedDescription = description?.trim();
  return trimmedDescription ? `${label} ${trimmedDescription}` : label;
}
