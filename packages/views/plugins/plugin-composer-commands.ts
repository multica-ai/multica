import type {
  PluginComposerCommand,
  PluginComposerCommandContext,
  PluginInstallation,
  PluginSurface,
} from "@multica/core/types/plugin";

export type PluginComposerContext = PluginComposerCommandContext;

export interface PluginComposerCommandTarget {
  /** Stable within a workspace installation, even when plugins reuse keys. */
  id: string;
  installation: PluginInstallation;
  command: PluginComposerCommand;
  surface: PluginSurface;
}

function compareText(left: string, right: string): number {
  return left < right ? -1 : left > right ? 1 : 0;
}

function compareTargets(
  left: PluginComposerCommandTarget,
  right: PluginComposerCommandTarget,
): number {
  const leftContexts = [...left.command.contexts].sort(compareText).join("\u0000");
  const rightContexts = [...right.command.contexts].sort(compareText).join("\u0000");
  return compareText(left.id, right.id) ||
    compareText(left.installation.plugin_key, right.installation.plugin_key) ||
    compareText(left.command.label, right.command.label) ||
    compareText(left.command.description ?? "", right.command.description ?? "") ||
    compareText(leftContexts, rightContexts) ||
    compareText(left.surface.type, right.surface.type);
}

function supportsPlatform(surface: PluginSurface, platform: string | undefined): boolean {
  const platforms = surface.platforms ?? [];
  if (platforms.length === 0) return true;
  return platform !== undefined && platforms.includes(platform);
}

/**
 * Collects only commands that the current composer can safely offer.
 * The host remains responsible for invoking the referenced modal surface.
 */
export function collectComposerCommands(
  installations: readonly PluginInstallation[],
  context: PluginComposerContext,
  platform?: string,
): PluginComposerCommandTarget[] {
  const targets: PluginComposerCommandTarget[] = [];

  for (const installation of installations) {
    if (installation.enabled !== true || !installation.id || !installation.package_version_id) continue;

    for (const command of installation.composer_commands ?? []) {
      if (!command.key || !command.contexts?.includes(context)) continue;

      const matchingSurfaces = (installation.surfaces ?? []).filter(
        (surface) => surface.key === command.surface,
      );
      // A missing or ambiguous surface reference is not an available command.
      if (matchingSurfaces.length !== 1) continue;

      const surface = matchingSurfaces[0]!;
      if (surface.type !== "modal" || !supportsPlatform(surface, platform)) continue;

      targets.push({
        id: `${installation.id}:${command.key}`,
        installation,
        command,
        surface,
      });
    }
  }

  targets.sort(compareTargets);
  const seen = new Set<string>();
  return targets.filter((target) => {
    if (seen.has(target.id)) return false;
    seen.add(target.id);
    return true;
  });
}
