import type { CommandLine } from "electron";
import { readFileSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";

export function applyDesktopFlags(
  commandLine: Pick<CommandLine, "hasSwitch" | "appendSwitch">,
  options: { home?: string; configHome?: string; log?: Pick<Console, "info" | "warn"> } = {},
): void {
  const configHome = options.configHome ?? process.env.XDG_CONFIG_HOME;
  const path = join(configHome || join(options.home ?? homedir(), ".config"), "multica-flags.conf");
  const log = options.log ?? console;
  let contents: string;
  try {
    contents = readFileSync(path, "utf8");
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code !== "ENOENT") {
      log.warn(`[desktop-flags] Could not read ${path}; continuing without file switches`);
    }
    return;
  }

  const switches = new Map<string, string | undefined>();
  for (const [index, raw] of contents.split(/\r?\n/).entries()) {
    // Only surrounding switch-name whitespace is ignored; values stay literal.
    const line = raw.includes("=") ? raw.trimStart() : raw.trim();
    if (!line.trim() || line.startsWith("#")) continue;
    const match = /^--([a-zA-Z0-9][a-zA-Z0-9_-]*)(?:=(.*))?$/.exec(line);
    const name = match?.[1]?.toLowerCase();
    if (!name || line.includes("\0") || name.startsWith("multica-") || name.startsWith("appimage-")) {
      log.warn(`[desktop-flags] Ignored invalid or application-owned argument at ${path}:${index + 1}`);
      continue;
    }
    switches.set(name, match![2]);
  }

  for (const [name, value] of switches) {
    if (commandLine.hasSwitch(name)) {
      log.info(`[desktop-flags] Kept command-line --${name} ahead of ${path}`);
      continue;
    }
    try {
      if (value === undefined) commandLine.appendSwitch(name);
      else commandLine.appendSwitch(name, value);
      log.info(`[desktop-flags] Applied --${name} from ${path}`);
    } catch {
      log.warn(`[desktop-flags] Could not apply --${name} from ${path}; continuing`);
    }
  }
}
