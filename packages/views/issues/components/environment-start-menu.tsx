"use client";

import { Fragment } from "react";
import type { Agent, DeclaredEnvironment, IssueEnvironmentConfig } from "@multica/core/types";
import { Bot, Plus, Server } from "@multica/ui/icons";
import { IconButton } from "@multica/ui/components/common/icon-button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { useT } from "../../i18n";

export type DeclaredState = "available" | "running" | "starting";

export interface StartMenuMachine {
  title: string;
  offline: boolean;
}

const NOTE_CLASS = "px-2 py-1.5 text-caption text-muted-foreground";

/**
 * The section's "+": every environment the latest run on each machine
 * declared, started from that run's code, and a way to have an agent prepare
 * the code and start it when there is nothing to start directly.
 */
export function EnvironmentStartMenu({
  configs,
  machineFor,
  runLabel,
  declaredState,
  onStart,
  agent,
  agentChoices,
  preparing,
  onAskAgent,
}: {
  configs: IssueEnvironmentConfig[];
  machineFor: (runtimeId: string) => StartMenuMachine;
  runLabel: (taskId: string) => string | null;
  declaredState: (config: IssueEnvironmentConfig, declared: DeclaredEnvironment) => DeclaredState;
  onStart: (config: IssueEnvironmentConfig, declared: DeclaredEnvironment) => void;
  /** Who the start goes to; null lets the reader pick from `agentChoices`. */
  agent: Agent | null;
  agentChoices: Agent[];
  preparing: boolean;
  onAskAgent: (agent: Agent) => void;
}) {
  const { t } = useT("issues");
  const declaring = configs.filter((config) => config.environments.length > 0);
  const askLabel = agent
    ? t(($) => $.environments.start_menu.ask_agent, { name: agent.name })
    : t(($) => $.environments.start_menu.ask_any_agent);
  const askSide = preparing ? t(($) => $.environments.start_menu.preparing) : t(($) => $.environments.start_menu.new_run);

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <IconButton variant="ghost" size="icon-xs" label={t(($) => $.environments.start_menu.trigger)}>
            <Plus />
          </IconButton>
        }
      />
      <DropdownMenuContent align="end" className="w-80 max-w-(--available-width)">
        {declaring.map((config, index) => {
          const machine = machineFor(config.runtime_id);
          const run = runLabel(config.task_id);
          return (
            <Fragment key={config.runtime_id}>
              {index > 0 && <DropdownMenuSeparator />}
              <DropdownMenuGroup>
                <DropdownMenuLabel className="flex min-w-0 items-center gap-1.5">
                  <span className="min-w-0 truncate">{run ? `${machine.title} · ${run}` : machine.title}</span>
                  {machine.offline && (
                    <span className="shrink-0 rounded-xs bg-warning/10 px-1 text-micro font-medium text-warning">{t(($) => $.environments.offline)}</span>
                  )}
                </DropdownMenuLabel>
                {config.environments.map((declared) => {
                  const state = declaredState(config, declared);
                  const disabled = state !== "available" || machine.offline || !config.can_control;
                  return (
                    <DropdownMenuItem
                      key={`${declared.repo}/${declared.name}`}
                      disabled={disabled}
                      onClick={() => onStart(config, declared)}
                    >
                      <Server className="text-muted-foreground" />
                      <span className="min-w-0 truncate">{declared.label || declared.name}</span>
                      <span className="ml-auto max-w-[50%] shrink-0 truncate pl-3 text-caption text-muted-foreground">
                        {state === "running"
                          ? t(($) => $.environments.state.running)
                          : state === "starting"
                            ? t(($) => $.environments.state.starting)
                            : <code className="font-mono">{declared.start}</code>}
                      </span>
                    </DropdownMenuItem>
                  );
                })}
              </DropdownMenuGroup>
            </Fragment>
          );
        })}
        {declaring.length > 0 ? (
          <p className={NOTE_CLASS}>
            {declaring.some((config) => !config.can_control)
              ? t(($) => $.environments.start_menu.no_permission)
              : t(($) => $.environments.start_menu.note)}
          </p>
        ) : configs.length === 0 ? (
          <p className={NOTE_CLASS}>{t(($) => $.environments.start_menu.nothing_to_start)}</p>
        ) : null}
        {(declaring.length > 0 || configs.length === 0) && <DropdownMenuSeparator />}
        {agent ? (
          <DropdownMenuItem disabled={preparing} onClick={() => onAskAgent(agent)}>
            <Bot className="text-muted-foreground" />
            <span className="min-w-0 truncate">{askLabel}</span>
            <span className="ml-auto shrink-0 pl-3 text-caption text-muted-foreground">{askSide}</span>
          </DropdownMenuItem>
        ) : (
          <DropdownMenuSub>
            <DropdownMenuSubTrigger disabled={preparing || agentChoices.length === 0}>
              <Bot className="text-muted-foreground" />
              <span className="min-w-0 truncate">{askLabel}</span>
            </DropdownMenuSubTrigger>
            <DropdownMenuSubContent className="max-h-72 w-56">
              {agentChoices.map((choice) => (
                <DropdownMenuItem key={choice.id} onClick={() => onAskAgent(choice)}>
                  <span className="min-w-0 truncate">{choice.name}</span>
                </DropdownMenuItem>
              ))}
            </DropdownMenuSubContent>
          </DropdownMenuSub>
        )}
        {declaring.length === 0 && (
          <p className={NOTE_CLASS}>
            {agent
              ? t(($) => $.environments.start_menu.ask_agent_hint, { name: agent.name })
              : t(($) => $.environments.start_menu.ask_any_agent_hint)}
          </p>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
