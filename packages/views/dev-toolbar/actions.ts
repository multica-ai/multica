import type { QueryClient } from "@tanstack/react-query";
import type { User } from "@multica/core/types";
import type { MetricTone } from "./metrics";

/** What an action can read and act on when it runs. */
export interface DevActionContext {
  wsId: string | null;
  user: User | null;
  queryClient: QueryClient;
}

export interface DevActionChoice {
  value: string;
  label: string;
}

export interface DevAction {
  id: string;
  title: string;
  description?: string;
  /** Runs only with a workspace open, or only signed in. */
  needs?: "workspace" | "user";
  /** Shown as a warning, for actions that touch real data. */
  caution?: string;
  /** Values the action runs with; each one is a command of its own. */
  choices?: readonly DevActionChoice[];
  run: (ctx: DevActionContext, choice?: string) => void;
}

export interface DevFeatureState {
  status?: { text: string; tone?: MetricTone };
  rows?: readonly { label: string; value: string; tone?: MetricTone }[];
  note?: string;
}

/**
 * A feature's dev actions, declared next to the feature's own code. The
 * toolbar lists every feature in `registry.ts` and imports nothing else from
 * it, so production bundles, which never load the toolbar, leave these out.
 */
export interface DevFeature {
  id: string;
  /** Section of the Actions panel, shared by related features. */
  group: string;
  title: string;
  /** Live state, read with hooks; the panel calls it on every render of the feature. */
  useDevState?: (ctx: DevActionContext) => DevFeatureState;
  actions: readonly DevAction[];
}

/** One runnable thing: an action, or one of its choices. */
export interface DevCommand {
  key: string;
  feature: DevFeature;
  action: DevAction;
  choice?: DevActionChoice;
}

export function devCommands(features: readonly DevFeature[]): DevCommand[] {
  return features.flatMap((feature) =>
    feature.actions.flatMap((action) => {
      const key = `${feature.id}/${action.id}`;
      if (!action.choices) return [{ key, feature, action }];
      return action.choices.map((choice) => ({ key: `${key}=${choice.value}`, feature, action, choice }));
    }),
  );
}

export const commandTitle = (command: DevCommand) =>
  command.choice ? `${command.action.title} → ${command.choice.label}` : command.action.title;

export function blockedReason(action: DevAction, ctx: DevActionContext): string | null {
  if (action.needs === "workspace" && !ctx.wsId) return "needs a workspace";
  if (action.needs === "user" && !ctx.user) return "needs a signed-in user";
  return null;
}

/**
 * Commands whose words contain every word of `query`, those whose title
 * starts with it first. Words match anywhere in the group, feature, title,
 * choice or description.
 */
export function searchCommands(commands: readonly DevCommand[], query: string): DevCommand[] {
  const words = query.toLowerCase().split(/\s+/).filter(Boolean);
  if (words.length === 0) return [];
  const q = words.join(" ");
  return commands
    .map((command, index) => {
      const title = commandTitle(command).toLowerCase();
      const haystack = [command.feature.group, command.feature.title, title, command.action.description ?? ""]
        .join(" ")
        .toLowerCase();
      if (!words.every((word) => haystack.includes(word))) return null;
      const rank = title.startsWith(q) ? 0 : title.includes(q) ? 1 : 2;
      return { command, rank, index };
    })
    .filter((hit) => hit !== null)
    .sort((a, b) => a.rank - b.rank || a.index - b.index)
    .map((hit) => hit.command);
}
