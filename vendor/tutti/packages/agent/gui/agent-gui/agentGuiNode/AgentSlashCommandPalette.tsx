import { ConnectorPaletteItem } from "@tutti-os/connector-renderer/ui";
import { Spinner } from "@tutti-os/ui-system";
import {
  Globe,
  Info,
  ListChecks,
  MessageCirclePlus,
  Minimize2,
  Monitor,
  Search,
  Target,
  ZapIcon
} from "lucide-react";
import { observer } from "mobx-react-lite";
import { Fragment, useEffect, useRef, type ReactNode } from "react";
import { cn } from "../../app/renderer/lib/utils";
import type { AgentSessionCommand } from "../../shared/agentSessionTypes";
import { projectConnectorPaletteItem } from "./integrations/connector/model/connectorPresentation";
import type { AgentGUIProviderSkillOption } from "./model/agentGuiNodeTypes";
import type { AgentSlashCommandCapability } from "./model/agentSlashCommandProviderPolicy";

export type AgentSlashPaletteEntry =
  | {
      type: "command";
      key: string;
      label: string;
      primaryLabel?: string;
      secondaryLabel?: string;
      description?: string;
      command: AgentSessionCommand;
    }
  | {
      type: "capability";
      key: string;
      label: string;
      description?: string;
      settingsAriaLabel?: string;
      settingsLabel?: string;
      disabled?: boolean;
      selectAction?: "capability" | "settings";
      capability: AgentSlashCommandCapability;
    }
  | {
      type: "skill";
      key: string;
      label: string;
      description?: string;
      skill: AgentGUIProviderSkillOption;
    };

interface AgentSlashCommandPaletteProps {
  entries: readonly AgentSlashPaletteEntry[];
  highlightedIndex: number;
  label: string;
  commandsGroupLabel: string;
  capabilitiesGroupLabel: string;
  capabilitiesLoading?: boolean;
  capabilitiesLoadingLabel?: string;
  skillsGroupLabel: string;
  pluginsGroupLabel: string;
  connectorsGroupLabel: string;
  connectorConnectedLabel: string;
  connectorNotConnectedLabel: string;
  connectorUnsupportedLabel: string;
  mcpGroupLabel: string;
  onHighlightChange: (index: number) => void;
  onSelect: (command: AgentSessionCommand) => void;
  onSelectCapability: (capability: AgentSlashCommandCapability) => void;
  onSelectCapabilitySettings?: (
    capability: AgentSlashCommandCapability
  ) => void;
  onSelectSkill: (skill: AgentGUIProviderSkillOption) => void;
}

const paletteStyles = {
  palette:
    "nodrag agent-gui-node__mention-palette flex h-full min-h-0 flex-col gap-0.5 overflow-y-auto px-1 pb-1 pt-2 [-webkit-app-region:no-drag]",
  option:
    "nodrag relative flex h-7 min-h-7 w-full min-w-0 cursor-pointer select-none items-center gap-2 overflow-hidden rounded-[6px] border-0 bg-transparent px-2.5 py-0 text-left text-[13px] text-[var(--text-primary)] outline-hidden transition-colors duration-200 [-webkit-app-region:no-drag] hover:bg-[var(--transparency-block)] focus-visible:bg-[var(--transparency-block)] focus-visible:outline-none active:bg-[var(--transparency-active)] data-[highlighted]:bg-[var(--transparency-block)] data-[highlighted]:text-[var(--text-primary)] [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4",
  icon: "flex w-4 shrink-0 items-center justify-center self-center text-[var(--text-secondary)]",
  copy: "flex min-w-0 flex-1 items-center gap-[8px] overflow-hidden leading-[16px]",
  name: "flex min-w-0 max-w-[48%] shrink-0 items-center gap-[8px] overflow-hidden",
  primaryName:
    "min-w-0 truncate text-[13px] font-semibold text-[var(--text-primary)]",
  secondaryName:
    "shrink-0 text-[13px] font-normal text-[var(--text-secondary)]",
  descriptionText:
    "min-w-0 flex-1 truncate text-[13px] font-normal text-[var(--text-secondary)]",
  groupHeader:
    "select-none px-2.5 pb-0.5 text-[11px] font-normal text-[var(--text-secondary)]",
  groupHeaderFirst: "pt-1.5",
  groupHeaderSeparated:
    "relative mt-3 pt-3 before:absolute before:inset-x-3 before:top-0 before:border-t before:border-[var(--border-1)] before:content-['']",
  settingsButton:
    "nodrag ml-1 flex h-5 min-h-5 shrink-0 items-center rounded-[4px] border-0 bg-[var(--transparency-hover)] px-2 py-0 text-[11px] font-semibold leading-[14px] text-[var(--text-secondary)] outline-none transition-colors duration-150 hover:bg-[var(--transparency-active)] hover:text-[var(--text-primary)] focus-visible:ring-2 focus-visible:ring-[var(--agent-gui-focus-ring,var(--border-focus))]",
  loading:
    "flex min-h-9 w-full items-center gap-2 px-2.5 py-2 text-[11px] text-[var(--text-secondary)]"
};

export const AgentSlashCommandPalette = observer(
  function AgentSlashCommandPalette({
    entries,
    highlightedIndex,
    label,
    commandsGroupLabel,
    capabilitiesGroupLabel,
    capabilitiesLoading = false,
    capabilitiesLoadingLabel = "",
    skillsGroupLabel,
    pluginsGroupLabel,
    connectorsGroupLabel,
    connectorConnectedLabel,
    connectorNotConnectedLabel,
    connectorUnsupportedLabel,
    mcpGroupLabel,
    onHighlightChange,
    onSelect,
    onSelectCapability,
    onSelectCapabilitySettings,
    onSelectSkill
  }: AgentSlashCommandPaletteProps): React.JSX.Element | null {
    "use memo";
    const highlightedOptionRef = useRef<HTMLDivElement | null>(null);

    useEffect(() => {
      highlightedOptionRef.current?.scrollIntoView({ block: "nearest" });
    }, [highlightedIndex]);

    if (entries.length === 0 && !capabilitiesLoading) {
      return null;
    }
    // Headers render only when multiple sections are present, except
    // capabilities always keep their category label for discoverability. They are
    // plain separators outside the option list, so keyboard navigation indices
    // are untouched.
    const entryTypes = new Set(entries.map((entry) => entryGroupType(entry)));
    if (capabilitiesLoading) {
      entryTypes.add("capability");
    }
    const showGroupHeaders =
      entryTypes.size > 1 || entryTypes.has("capability");
    const firstEntryIndexByType = new Map<
      AgentSlashPaletteEntryGroup,
      number
    >();
    entries.forEach((entry, index) => {
      const groupType = entryGroupType(entry);
      if (!firstEntryIndexByType.has(groupType)) {
        firstEntryIndexByType.set(groupType, index);
      }
    });
    const hasCapabilityEntries = entries.some(
      (entry) => entry.type === "capability"
    );
    const capabilityLoadingInsertIndex = capabilitiesLoading
      ? capabilityLoadingIndex(entries)
      : -1;
    const capabilityLoadingNode = capabilitiesLoading ? (
      <Fragment key="capability-loading">
        {!hasCapabilityEntries ? (
          <div
            aria-hidden="true"
            className={cn(
              paletteStyles.groupHeader,
              capabilityLoadingInsertIndex === 0
                ? paletteStyles.groupHeaderFirst
                : paletteStyles.groupHeaderSeparated
            )}
          >
            {capabilitiesGroupLabel}
          </div>
        ) : null}
        <div aria-live="polite" className={paletteStyles.loading} role="status">
          <span aria-hidden="true" className={paletteStyles.icon}>
            <Spinner size={12} strokeWidth={2} />
          </span>
          <span>{capabilitiesLoadingLabel}</span>
        </div>
      </Fragment>
    ) : null;
    return (
      <div className={paletteStyles.palette} role="listbox" aria-label={label}>
        {entries.map((entry, index) => {
          const isHighlighted = index === highlightedIndex;
          const connectorItem =
            entry.type === "skill"
              ? projectConnectorPaletteItem(
                  entry.skill,
                  entry.label,
                  entry.description
                    ? descriptionWithoutTerminalPeriod(entry.description)
                    : undefined
                )
              : null;
          const isDisabled =
            (entry.type === "capability" && entry.disabled === true) ||
            connectorItem?.status === "unsupported";
          const groupType = entryGroupType(entry);
          const entryIcon = connectorItem ? null : slashPaletteEntryIcon(entry);
          const groupHeader =
            showGroupHeaders &&
            firstEntryIndexByType.get(groupType) === index ? (
              <div
                aria-hidden="true"
                className={cn(
                  paletteStyles.groupHeader,
                  index === 0 && capabilityLoadingInsertIndex !== 0
                    ? paletteStyles.groupHeaderFirst
                    : paletteStyles.groupHeaderSeparated
                )}
              >
                {labelForEntryGroupType(groupType, {
                  commandsGroupLabel,
                  capabilitiesGroupLabel,
                  skillsGroupLabel,
                  pluginsGroupLabel,
                  connectorsGroupLabel,
                  mcpGroupLabel
                })}
              </div>
            ) : null;
          return (
            <Fragment key={entry.key}>
              {capabilityLoadingInsertIndex === index
                ? capabilityLoadingNode
                : null}
              {groupHeader}
              <div
                ref={isHighlighted ? highlightedOptionRef : null}
                className={cn(
                  paletteStyles.option,
                  isHighlighted && "bg-[var(--transparency-block)]",
                  isDisabled &&
                    "cursor-not-allowed text-[var(--agent-gui-text-tertiary)] opacity-60 hover:bg-transparent active:bg-transparent"
                )}
                role="option"
                aria-selected={isHighlighted}
                aria-disabled={isDisabled || undefined}
                data-testid={
                  entry.type === "command"
                    ? `agent-gui-composer-slash-command-${entry.command.name}`
                    : undefined
                }
                data-highlighted={isHighlighted ? "" : undefined}
                onMouseEnter={() => onHighlightChange(index)}
                onMouseDown={(event) => event.preventDefault()}
                onClick={() => {
                  if (isDisabled) {
                    return;
                  }
                  if (entry.type === "command") {
                    onSelect(entry.command);
                    return;
                  }
                  if (entry.type === "capability") {
                    if (entry.selectAction === "settings") {
                      onSelectCapabilitySettings?.(entry.capability);
                      return;
                    }
                    onSelectCapability(entry.capability);
                    return;
                  }
                  onSelectSkill(entry.skill);
                }}
              >
                {connectorItem && entry.type === "skill" ? (
                  <ConnectorPaletteItem
                    item={connectorItem}
                    labels={{
                      connected: connectorConnectedLabel,
                      notConnected: connectorNotConnectedLabel,
                      unsupported: connectorUnsupportedLabel
                    }}
                    onSetup={() => onSelectSkill(entry.skill)}
                  />
                ) : (
                  <>
                    {entryIcon ? (
                      <span aria-hidden="true" className={paletteStyles.icon}>
                        {entryIcon}
                      </span>
                    ) : null}
                    <span className={paletteStyles.copy}>
                      <span className={paletteStyles.name}>
                        <span className={paletteStyles.primaryName}>
                          {entry.type === "command"
                            ? (entry.primaryLabel ?? entry.label)
                            : entry.label}
                        </span>
                        {entry.type === "command" && entry.secondaryLabel ? (
                          <span className={paletteStyles.secondaryName}>
                            {entry.secondaryLabel}
                          </span>
                        ) : null}
                      </span>
                      {entry.description ? (
                        <span className={paletteStyles.descriptionText}>
                          {descriptionWithoutTerminalPeriod(entry.description)}
                        </span>
                      ) : null}
                    </span>
                  </>
                )}
                {entry.type === "capability" &&
                entry.settingsLabel &&
                onSelectCapabilitySettings ? (
                  <button
                    aria-label={entry.settingsAriaLabel ?? entry.settingsLabel}
                    className={paletteStyles.settingsButton}
                    disabled={isDisabled}
                    title={entry.settingsAriaLabel ?? entry.settingsLabel}
                    type="button"
                    onClick={(event) => {
                      event.stopPropagation();
                      onSelectCapabilitySettings(entry.capability);
                    }}
                    onMouseDown={(event) => {
                      event.preventDefault();
                      event.stopPropagation();
                    }}
                  >
                    {entry.settingsLabel}
                  </button>
                ) : null}
              </div>
            </Fragment>
          );
        })}
        {capabilityLoadingInsertIndex === entries.length
          ? capabilityLoadingNode
          : null}
      </div>
    );
  }
);

function descriptionWithoutTerminalPeriod(description: string): string {
  return description.trimEnd().replace(/[。.]+$/u, "");
}

type AgentSlashPaletteEntryGroup =
  | "command"
  | "capability"
  | "skill"
  | "plugin"
  | "connector"
  | "mcp";

function capabilityLoadingIndex(
  entries: readonly AgentSlashPaletteEntry[]
): number {
  let lastCapabilityIndex = -1;
  for (let index = entries.length - 1; index >= 0; index -= 1) {
    if (entries[index]?.type === "capability") {
      lastCapabilityIndex = index;
      break;
    }
  }
  if (lastCapabilityIndex >= 0) {
    return lastCapabilityIndex + 1;
  }
  const firstSkillIndex = entries.findIndex((entry) => entry.type === "skill");
  return firstSkillIndex >= 0 ? firstSkillIndex : entries.length;
}

function entryGroupType(
  entry: AgentSlashPaletteEntry
): AgentSlashPaletteEntryGroup {
  if (entry.type !== "skill") {
    return entry.type;
  }
  if (
    entry.skill.sourceKind === "connector" ||
    entry.skill.kind === "connector"
  ) {
    return "connector";
  }
  if (entry.skill.kind === "skill") {
    return "skill";
  }
  if (entry.skill.sourceKind === "plugin") {
    return "plugin";
  }
  return "skill";
}

function labelForEntryGroupType(
  groupType: AgentSlashPaletteEntryGroup,
  labels: {
    commandsGroupLabel: string;
    capabilitiesGroupLabel: string;
    skillsGroupLabel: string;
    pluginsGroupLabel: string;
    connectorsGroupLabel: string;
    mcpGroupLabel: string;
  }
): string {
  switch (groupType) {
    case "command":
      return labels.commandsGroupLabel;
    case "capability":
      return labels.capabilitiesGroupLabel;
    case "plugin":
      return labels.pluginsGroupLabel;
    case "connector":
      return labels.connectorsGroupLabel;
    case "mcp":
      return labels.mcpGroupLabel;
    case "skill":
      return labels.skillsGroupLabel;
  }
}

// Keep the explicit `size-*` class on each icon so the palette option's
// `[&_svg:not([class*='size-'])]:size-4` fallback does not override it.
const SLASH_PALETTE_ICON_CLASS = "size-4";

function slashPaletteEntryIcon(entry: AgentSlashPaletteEntry): ReactNode {
  if (entry.type === "capability") {
    return entry.capability.capability === "computerUse" ? (
      <Monitor className={SLASH_PALETTE_ICON_CLASS} />
    ) : (
      <Globe className={SLASH_PALETTE_ICON_CLASS} />
    );
  }
  if (entry.type !== "command") {
    return null;
  }
  switch (entry.command.name.trim().toLowerCase()) {
    case "compact":
      return <Minimize2 className={SLASH_PALETTE_ICON_CLASS} />;
    case "fast":
      return <ZapIcon className={SLASH_PALETTE_ICON_CLASS} />;
    case "goal":
      return <Target className={SLASH_PALETTE_ICON_CLASS} />;
    case "plan":
      return <ListChecks className={SLASH_PALETTE_ICON_CLASS} />;
    case "side":
      return <MessageCirclePlus className={SLASH_PALETTE_ICON_CLASS} />;
    case "review":
      return <Search className={SLASH_PALETTE_ICON_CLASS} />;
    case "status":
      return <Info className={SLASH_PALETTE_ICON_CLASS} />;
    default:
      return null;
  }
}
