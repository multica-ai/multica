import type { UiLanguage } from "../../../contexts/settings/domain/agentSettings";
import type { AgentSessionCommand } from "../../../shared/agentSessionTypes";
import { labelForSlashCommand } from "../model/agentSlashCommands";
import type { AgentComposerRenderProps as AgentComposerProps } from "./AgentComposer.types";

export function slashCommandDescriptionForDisplay(
  command: AgentSessionCommand,
  labels: Pick<
    AgentComposerProps["labels"],
    | "slashCommandPresentation"
    | "slashCommandCompactDescription"
    | "slashCommandContextDescription"
    | "slashCommandFastDescription"
    | "slashCommandInitDescription"
    | "slashCommandPlanDescription"
    | "slashCommandReviewDescription"
    | "slashCommandStatusDescription"
    | "slashCommandUsageDescription"
  >
): string | undefined {
  const providerDescription = command.description?.trim()
    ? command.description
    : undefined;
  const localizedDescription = labels.slashCommandPresentation?.(
    command.name
  ).description;
  if (localizedDescription) {
    return providerDescription ?? localizedDescription;
  }
  switch (command.name.trim().toLowerCase()) {
    case "compact":
      return labels.slashCommandCompactDescription;
    case "context":
      return labels.slashCommandContextDescription;
    case "fast":
      return labels.slashCommandFastDescription;
    case "init":
      return labels.slashCommandInitDescription;
    case "plan":
      return labels.slashCommandPlanDescription;
    case "review":
      return labels.slashCommandReviewDescription;
    case "status":
      return labels.slashCommandStatusDescription;
    case "usage":
      return labels.slashCommandUsageDescription;
    default:
      return providerDescription;
  }
}

export function slashCommandLabelForDisplay(
  command: AgentSessionCommand,
  labels: Pick<
    AgentComposerProps["labels"],
    | "slashCommandPresentation"
    | "slashCommandCompactLabel"
    | "slashCommandContextLabel"
    | "slashCommandFastLabel"
    | "slashCommandInitLabel"
    | "slashCommandPlanLabel"
    | "slashCommandReviewLabel"
    | "slashCommandStatusLabel"
    | "slashCommandUsageLabel"
  >,
  uiLanguage: UiLanguage
): { primaryLabel?: string; secondaryLabel?: string } {
  const canonicalLabel = labelForSlashCommand(command);
  const primaryLabel = localizedSlashCommandLabel(command, labels);
  return uiLanguage === "en" || primaryLabel === canonicalLabel
    ? { primaryLabel }
    : { primaryLabel, secondaryLabel: canonicalLabel };
}

function localizedSlashCommandLabel(
  command: AgentSessionCommand,
  labels: Pick<
    AgentComposerProps["labels"],
    | "slashCommandPresentation"
    | "slashCommandCompactLabel"
    | "slashCommandContextLabel"
    | "slashCommandFastLabel"
    | "slashCommandInitLabel"
    | "slashCommandPlanLabel"
    | "slashCommandReviewLabel"
    | "slashCommandStatusLabel"
    | "slashCommandUsageLabel"
  >
): string {
  const localizedLabel = labels.slashCommandPresentation?.(command.name).label;
  if (localizedLabel) {
    return localizedLabel;
  }
  switch (command.name.trim().toLowerCase()) {
    case "compact":
      return labels.slashCommandCompactLabel;
    case "context":
      return labels.slashCommandContextLabel;
    case "fast":
      return labels.slashCommandFastLabel;
    case "init":
      return labels.slashCommandInitLabel;
    case "plan":
      return labels.slashCommandPlanLabel;
    case "review":
      return labels.slashCommandReviewLabel;
    case "status":
      return labels.slashCommandStatusLabel;
    case "usage":
      return labels.slashCommandUsageLabel;
    default:
      return labelForSlashCommand(command);
  }
}
