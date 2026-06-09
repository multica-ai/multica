#!/usr/bin/env node
import { spawnSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";

const DEFAULT_NAMESPACE = "multica-bot";
const DEFAULT_SECRET = "multica-bot-secrets";
const DEFAULT_ENV_FILE = ".env.bot";
const DEFAULT_ROLLOUTS = ["backend", "frontend"];
const DEFAULT_TIMEOUT = "180s";

export function parseDotenv(source, sourceName = DEFAULT_ENV_FILE) {
  const entries = new Map();
  const seen = new Map();
  const lines = source.replace(/^\uFEFF/, "").split(/\r?\n/);

  lines.forEach((originalLine, index) => {
    const lineNumber = index + 1;
    let line = originalLine.trim();

    if (!line || line.startsWith("#")) {
      return;
    }

    if (line.startsWith("export ")) {
      line = line.slice("export ".length).trimStart();
    }

    const separatorIndex = line.indexOf("=");
    if (separatorIndex === -1) {
      throw new Error(`${sourceName}:${lineNumber}: expected KEY=VALUE`);
    }

    const key = line.slice(0, separatorIndex).trim();
    if (!/^[A-Za-z_][A-Za-z0-9_]*$/.test(key)) {
      throw new Error(`${sourceName}:${lineNumber}: invalid env key "${key}"`);
    }

    if (seen.has(key)) {
      throw new Error(
        `${sourceName}:${lineNumber}: duplicate env key "${key}" also defined on line ${seen.get(
          key,
        )}`,
      );
    }

    const value = parseDotenvValue(
      line.slice(separatorIndex + 1),
      sourceName,
      lineNumber,
    );
    entries.set(key, value);
    seen.set(key, lineNumber);
  });

  return entries;
}

export function buildSecretManifest(entries, namespace, secretName) {
  return {
    apiVersion: "v1",
    kind: "Secret",
    metadata: {
      name: secretName,
      namespace,
      labels: {
        "app.kubernetes.io/managed-by": "multica-bot-env-sync",
      },
    },
    type: "Opaque",
    stringData: Object.fromEntries(entries),
  };
}

function parseDotenvValue(rawValue, sourceName, lineNumber) {
  const value = rawValue.trimStart();
  if (value === "") {
    return "";
  }

  const quote = value[0];
  if (quote === "'" || quote === '"' || quote === "`") {
    return parseQuotedValue(value, quote, sourceName, lineNumber);
  }

  let unquoted = value.trimEnd();
  for (let index = 0; index < unquoted.length; index += 1) {
    if (unquoted[index] === "#" && (index === 0 || /\s/.test(unquoted[index - 1]))) {
      unquoted = unquoted.slice(0, index).trimEnd();
      break;
    }
  }
  return unquoted;
}

function parseQuotedValue(rawValue, quote, sourceName, lineNumber) {
  let result = "";

  for (let index = 1; index < rawValue.length; index += 1) {
    const char = rawValue[index];

    if (char === quote) {
      const trailing = rawValue.slice(index + 1).trim();
      if (trailing && !trailing.startsWith("#")) {
        throw new Error(
          `${sourceName}:${lineNumber}: unexpected text after quoted value`,
        );
      }
      return result;
    }

    if (quote === '"' && char === "\\" && index + 1 < rawValue.length) {
      const next = rawValue[index + 1];
      const escapes = new Map([
        ["n", "\n"],
        ["r", "\r"],
        ["t", "\t"],
        ['"', '"'],
        ["\\", "\\"],
        ["$", "$"],
      ]);
      result += escapes.get(next) ?? next;
      index += 1;
      continue;
    }

    result += char;
  }

  throw new Error(`${sourceName}:${lineNumber}: unterminated quoted value`);
}

function parseArgs(argv) {
  const options = {
    envFile: DEFAULT_ENV_FILE,
    namespace: DEFAULT_NAMESPACE,
    secretName: DEFAULT_SECRET,
    kubectl: "kubectl",
    dryRun: false,
    noRollout: false,
    rollouts: null,
    timeout: DEFAULT_TIMEOUT,
    help: false,
  };

  for (let index = 0; index < argv.length; index += 1) {
    const arg = argv[index];
    const [name, inlineValue] = arg.split("=", 2);

    switch (name) {
      case "--env-file":
        options.envFile = inlineValue ?? readRequiredValue(argv, ++index, arg);
        break;
      case "--namespace":
      case "-n":
        options.namespace = inlineValue ?? readRequiredValue(argv, ++index, arg);
        break;
      case "--secret":
        options.secretName = inlineValue ?? readRequiredValue(argv, ++index, arg);
        break;
      case "--kubectl":
        options.kubectl = inlineValue ?? readRequiredValue(argv, ++index, arg);
        break;
      case "--rollout": {
        const rollout = inlineValue ?? readRequiredValue(argv, ++index, arg);
        options.rollouts = options.rollouts ?? [];
        options.rollouts.push(rollout);
        break;
      }
      case "--timeout":
        options.timeout = inlineValue ?? readRequiredValue(argv, ++index, arg);
        break;
      case "--dry-run":
        options.dryRun = true;
        break;
      case "--no-rollout":
        options.noRollout = true;
        break;
      case "--help":
      case "-h":
        options.help = true;
        break;
      default:
        throw new Error(`unknown argument: ${arg}`);
    }
  }

  return {
    ...options,
    rollouts: options.noRollout ? [] : (options.rollouts ?? DEFAULT_ROLLOUTS),
  };
}

function readRequiredValue(argv, index, argName) {
  const value = argv[index];
  if (!value || value.startsWith("-")) {
    throw new Error(`${argName} requires a value`);
  }
  return value;
}

function usage() {
  return `Usage:
  node k8s/bot/sync-env.mjs [options]

Options:
  --env-file <path>       Dotenv source file. Default: ${DEFAULT_ENV_FILE}
  --namespace, -n <name>  Kubernetes namespace. Default: ${DEFAULT_NAMESPACE}
  --secret <name>         Kubernetes Secret name. Default: ${DEFAULT_SECRET}
  --rollout <deployment>  Deployment to restart and wait for. Repeatable.
                          Default: ${DEFAULT_ROLLOUTS.join(", ")}
  --no-rollout            Apply the Secret without restarting deployments.
  --timeout <duration>    kubectl rollout status timeout. Default: ${DEFAULT_TIMEOUT}
  --kubectl <path>        kubectl executable. Default: kubectl
  --dry-run               Parse and report keys without calling kubectl.
  --help                  Show this help.

The script logs only key names, key counts, Secret apply status, and rollout
results. It never prints Secret values.`;
}

function main() {
  const options = parseArgs(process.argv.slice(2));
  if (options.help) {
    console.log(usage());
    return;
  }

  const envPath = resolve(options.envFile);
  const entries = parseDotenv(readFileSync(envPath, "utf8"), options.envFile);
  const keys = [...entries.keys()].sort();

  log(`loaded ${keys.length} keys from ${options.envFile}`);
  log(`keys: ${keys.join(", ")}`);

  if (options.dryRun) {
    log(
      `dry-run: would apply Secret ${options.secretName} in namespace ${options.namespace}`,
    );
    options.rollouts.forEach((deployment) => {
      log(`dry-run: would rollout deployment/${deployment}`);
    });
    return;
  }

  const manifest = buildSecretManifest(
    entries,
    options.namespace,
    options.secretName,
  );

  applySecret(options, manifest);
  options.rollouts.forEach((deployment) => rolloutDeployment(options, deployment));
}

function applySecret(options, manifest) {
  log(`applying Secret ${options.secretName} in namespace ${options.namespace}`);
  const result = runKubectl(options, ["apply", "-n", options.namespace, "-f", "-"], {
    input: `${JSON.stringify(manifest)}\n`,
  });

  if (result.status !== 0) {
    console.error(
      `[bot-env-sync] Secret apply failed with exit code ${result.status}; kubectl output suppressed because it may contain Secret values.`,
    );
    process.exit(result.status || 1);
  }

  log(`secret apply result: ${oneLine(result.stdout) || "ok"}`);
}

function rolloutDeployment(options, deployment) {
  const target = `deployment/${deployment}`;

  const restart = runKubectl(options, [
    "-n",
    options.namespace,
    "rollout",
    "restart",
    target,
  ]);
  if (restart.status !== 0) {
    failKubectl("rollout restart", target, restart);
  }
  log(`rollout restart ${target}: ${oneLine(restart.stdout) || "ok"}`);

  const status = runKubectl(options, [
    "-n",
    options.namespace,
    "rollout",
    "status",
    target,
    `--timeout=${options.timeout}`,
  ]);
  if (status.status !== 0) {
    failKubectl("rollout status", target, status);
  }
  log(`rollout status ${target}: ${oneLine(status.stdout) || "ok"}`);
}

function runKubectl(options, args, spawnOptions = {}) {
  const result = spawnSync(options.kubectl, args, {
    encoding: "utf8",
    maxBuffer: 10 * 1024 * 1024,
    ...spawnOptions,
  });

  if (result.error) {
    throw result.error;
  }

  return {
    status: result.status ?? 1,
    stdout: result.stdout ?? "",
    stderr: result.stderr ?? "",
  };
}

function failKubectl(action, target, result) {
  console.error(`[bot-env-sync] ${action} ${target} failed with exit code ${result.status}`);
  if (result.stdout.trim()) {
    console.error(`[bot-env-sync] stdout: ${oneLine(result.stdout)}`);
  }
  if (result.stderr.trim()) {
    console.error(`[bot-env-sync] stderr: ${oneLine(result.stderr)}`);
  }
  process.exit(result.status || 1);
}

function oneLine(output) {
  return output.trim().split(/\r?\n/).filter(Boolean).join(" | ");
}

function log(message) {
  console.log(`[bot-env-sync] ${message}`);
}

const currentFile = fileURLToPath(import.meta.url);
if (process.argv[1] && resolve(process.argv[1]) === currentFile) {
  try {
    main();
  } catch (error) {
    console.error(`[bot-env-sync] ${error.message}`);
    process.exit(1);
  }
}
