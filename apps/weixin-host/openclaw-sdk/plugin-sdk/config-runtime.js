import fs from "node:fs";
import os from "node:os";
import path from "node:path";

function configPath() {
  const explicit = process.env.OPENCLAW_CONFIG?.trim();
  if (explicit) return explicit;
  const stateDir = process.env.OPENCLAW_STATE_DIR?.trim() || path.join(os.homedir(), ".openclaw");
  return path.join(stateDir, "openclaw.json");
}

export function loadConfig() {
  try {
    return JSON.parse(fs.readFileSync(configPath(), "utf-8"));
  } catch {
    return {};
  }
}

export async function writeConfigFile(config) {
  const file = configPath();
  fs.mkdirSync(path.dirname(file), { recursive: true });
  fs.writeFileSync(file, `${JSON.stringify(config, null, 2)}\n`);
}
