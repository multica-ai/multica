// Curated capability catalog (FIR-2193, issue section 2).
//
// Before this, the New-grant dialog offered a flat list of seven free-form
// capability strings ("issue.read", "secret.use", …). That meant the actions
// users most expect to control — running a shell command, calling an external
// API, spending money — were not first-class choices, and the picker read like
// internal API jargon rather than "what is this agent allowed to do".
//
// This catalog makes the dangerous actions standard, named choices, each with
// a plain-language explanation the workspace admin can understand without
// knowing the engine's capability keys. The keys are the canonical capability
// strings the permission engine matches on (see
// server/internal/cerebro/permissions and permgate).
//
// "dangerous" entries are surfaced first in the picker because they are the
// ones an operator most wants to gate behind approval. "data" entries are the
// existing Multica-data capabilities, kept so nothing regresses.

export type CapabilityCategory = "dangerous" | "data";

export interface CapabilityCatalogEntry {
  /** Canonical capability key the permission engine matches on. */
  key: string;
  /** Short, plain-language name of the action. */
  label: string;
  /** One sentence explaining what the action does, in user language. */
  description: string;
  category: CapabilityCategory;
}

// The seven dangerous actions users expect to control, in the order the issue
// lists them. Keys use a dotted namespace so an admin can grant a whole family
// ("network.*") via the engine's prefix matching.
export const DANGEROUS_CAPABILITIES: CapabilityCatalogEntry[] = [
  {
    key: "shell.exec",
    label: "Run a shell command",
    description:
      "Run a command or script on the machine (for example via the terminal).",
    category: "dangerous",
  },
  {
    key: "network.external",
    label: "Call an external API or host",
    description:
      "Reach out to a website, API, or server outside Multica over the network.",
    category: "dangerous",
  },
  {
    key: "message.send",
    label: "Send a message out of the system",
    description:
      "Send an email, chat message, or other message to someone outside Multica.",
    category: "dangerous",
  },
  {
    key: "payment.commit",
    label: "Spend money or commit to a purchase",
    description:
      "Make a payment, place an order, or commit the company to a cost.",
    category: "dangerous",
  },
  {
    key: "deploy.restart",
    label: "Deploy or restart a service",
    description:
      "Ship code, deploy, or restart a running service in production.",
    category: "dangerous",
  },
  {
    key: "credentials.read",
    label: "Read or use a credential",
    description:
      "Read or use a stored secret, API key, password, or token.",
    category: "dangerous",
  },
  {
    key: "prod.write",
    label: "Write or change production data",
    description:
      "Create, change, or delete data in a live production system.",
    category: "dangerous",
  },
];

// Existing Multica-data capabilities, retained so current grants keep working.
export const DATA_CAPABILITIES: CapabilityCatalogEntry[] = [
  {
    key: "issue.read",
    label: "Read issues",
    description: "View issues and their details.",
    category: "data",
  },
  {
    key: "issue.write",
    label: "Create or edit issues",
    description: "Create, edit, comment on, or move issues.",
    category: "data",
  },
  {
    key: "project.read",
    label: "Read projects",
    description: "View projects and their contents.",
    category: "data",
  },
  {
    key: "project.write",
    label: "Create or edit projects",
    description: "Create or change projects.",
    category: "data",
  },
  {
    key: "agent.run",
    label: "Run agents",
    description: "Trigger an agent to run.",
    category: "data",
  },
  {
    key: "repo.push",
    label: "Push to a repository",
    description: "Push commits or open pull requests on a code repository.",
    category: "data",
  },
  {
    key: "secret.use",
    label: "Use a secret",
    description: "Use a stored secret in a workspace action.",
    category: "data",
  },
];

export const CAPABILITY_CATALOG: CapabilityCatalogEntry[] = [
  ...DANGEROUS_CAPABILITIES,
  ...DATA_CAPABILITIES,
];

/** Default capability the New-grant dialog selects (first sensitive action). */
export const DEFAULT_CAPABILITY_KEY = "shell.exec";

const CATALOG_BY_KEY: Record<string, CapabilityCatalogEntry> =
  Object.fromEntries(CAPABILITY_CATALOG.map((c) => [c.key, c]));

/** Look up a catalog entry by key, or undefined if it isn't a known capability. */
export function capabilityEntry(
  key: string,
): CapabilityCatalogEntry | undefined {
  return CATALOG_BY_KEY[key];
}

/**
 * Human label for a capability key. Falls back to the raw key for capabilities
 * created before the catalog existed (or via the API directly), so the UI never
 * renders blank.
 */
export function capabilityLabel(key: string): string {
  return CATALOG_BY_KEY[key]?.label ?? key;
}

/** Plain-language description for a capability key, or "" if unknown. */
export function capabilityDescription(key: string): string {
  return CATALOG_BY_KEY[key]?.description ?? "";
}
