#!/usr/bin/env node
/**
 * Agent Profile CLI
 *
 * Commands:
 *   new <id>     Create a new profile with default templates
 *   list         List all profiles
 *   show <id>    Show profile contents
 *   edit <id>    Open profile directory
 */

import { existsSync, readdirSync } from "node:fs";
import { join } from "node:path";
import {
  createAgentProfile,
  loadAgentProfile,
  getProfileDir,
  profileExists,
} from "../profile/index.js";
import { DATA_DIR } from "../../shared/index.js";

const DEFAULT_BASE_DIR = join(DATA_DIR, "agent-profiles");

type Command = "new" | "list" | "show" | "edit" | "help";

function printUsage() {
  console.log("Usage: pnpm profile <command> [options]");
  console.log("");
  console.log("Commands:");
  console.log("  new <id>     Create a new profile with default templates");
  console.log("  list         List all profiles");
  console.log("  show <id>    Show profile contents");
  console.log("  edit <id>    Open profile directory in Finder/file manager");
  console.log("  help         Show this help");
  console.log("");
  console.log("Examples:");
  console.log("  pnpm profile new my-agent");
  console.log("  pnpm profile list");
  console.log("  pnpm profile show my-agent");
}

function cmdNew(profileId: string | undefined) {
  if (!profileId) {
    console.error("Error: Profile ID is required");
    console.error("Usage: pnpm profile new <id>");
    process.exit(1);
  }

  // Validate profile ID
  if (!/^[a-zA-Z0-9_-]+$/.test(profileId)) {
    console.error("Error: Profile ID can only contain letters, numbers, hyphens, and underscores");
    process.exit(1);
  }

  if (profileExists(profileId)) {
    console.error(`Error: Profile "${profileId}" already exists`);
    console.error(`Location: ${getProfileDir(profileId)}`);
    process.exit(1);
  }

  const profile = createAgentProfile(profileId);
  const dir = getProfileDir(profileId);

  console.log(`Created profile: ${profile.id}`);
  console.log(`Location: ${dir}`);
  console.log("");
  console.log("Files created:");
  console.log("  - soul.md       (identity, personality and behavior)");
  console.log("  - user.md       (information about the user)");
  console.log("  - workspace.md  (workspace rules and conventions)");
  console.log("  - memory.md     (persistent knowledge)");
  console.log("");
  console.log("Edit these files to customize your agent, then run:");
  console.log(`  pnpm agent:cli --profile ${profileId} "Hello"`);
}

function cmdList() {
  if (!existsSync(DEFAULT_BASE_DIR)) {
    console.log("No profiles found.");
    console.log(`Create one with: pnpm profile new <id>`);
    return;
  }

  const entries = readdirSync(DEFAULT_BASE_DIR, { withFileTypes: true });
  const profiles = entries.filter((e) => e.isDirectory()).map((e) => e.name);

  if (profiles.length === 0) {
    console.log("No profiles found.");
    console.log(`Create one with: pnpm profile new <id>`);
    return;
  }

  console.log("Available profiles:");
  console.log("");
  for (const id of profiles) {
    const dir = getProfileDir(id);
    console.log(`  ${id}`);
    console.log(`    ${dir}`);
  }
  console.log("");
  console.log(`Total: ${profiles.length} profile(s)`);
}

function cmdShow(profileId: string | undefined) {
  if (!profileId) {
    console.error("Error: Profile ID is required");
    console.error("Usage: pnpm profile show <id>");
    process.exit(1);
  }

  const profile = loadAgentProfile(profileId);
  if (!profile) {
    console.error(`Error: Profile "${profileId}" not found`);
    console.error(`Create it with: pnpm profile new ${profileId}`);
    process.exit(1);
  }

  console.log(`Profile: ${profile.id}`);
  console.log(`Location: ${getProfileDir(profileId)}`);
  console.log("");

  if (profile.soul) {
    console.log("=== soul.md ===");
    console.log(profile.soul.trim());
    console.log("");
  }

  if (profile.user) {
    console.log("=== user.md ===");
    console.log(profile.user.trim());
    console.log("");
  }

  if (profile.workspace) {
    console.log("=== workspace.md ===");
    console.log(profile.workspace.trim());
    console.log("");
  }

  if (profile.memory) {
    console.log("=== memory.md ===");
    console.log(profile.memory.trim());
    console.log("");
  }
}

async function cmdEdit(profileId: string | undefined) {
  if (!profileId) {
    console.error("Error: Profile ID is required");
    console.error("Usage: pnpm profile edit <id>");
    process.exit(1);
  }

  if (!profileExists(profileId)) {
    console.error(`Error: Profile "${profileId}" not found`);
    console.error(`Create it with: pnpm profile new ${profileId}`);
    process.exit(1);
  }

  const dir = getProfileDir(profileId);
  const { spawn } = await import("node:child_process");

  // Open in default file manager
  const cmd = process.platform === "darwin" ? "open" : process.platform === "win32" ? "explorer" : "xdg-open";
  spawn(cmd, [dir], { detached: true, stdio: "ignore" }).unref();

  console.log(`Opened: ${dir}`);
}

async function main() {
  const args = process.argv.slice(2);
  const command = (args[0] || "help") as Command;
  const arg1 = args[1];

  switch (command) {
    case "new":
      cmdNew(arg1);
      break;
    case "list":
      cmdList();
      break;
    case "show":
      cmdShow(arg1);
      break;
    case "edit":
      await cmdEdit(arg1);
      break;
    case "help":
    default:
      printUsage();
      break;
  }
}

main().catch((err) => {
  console.error(err?.stack || String(err));
  process.exit(1);
});
