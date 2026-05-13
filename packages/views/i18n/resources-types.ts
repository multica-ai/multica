import "i18next";
// Pulls in the `ui` namespace augmentation owned by packages/ui — see
// packages/ui/types/i18next.ts. Side-effect import is required for views'
// typecheck program to see ui's contribution to `I18nResources`.
import "@multica/ui/i18n-types";
import type common from "../locales/en/common.json";
import type auth from "../locales/en/auth.json";
import type settings from "../locales/en/settings.json";
import type issues from "../locales/en/issues.json";
import type agents from "../locales/en/agents.json";
import type editor from "../locales/en/editor.json";
import type onboarding from "../locales/en/onboarding.json";
import type invite from "../locales/en/invite.json";
import type labels from "../locales/en/labels.json";
import type members from "../locales/en/members.json";
import type myIssues from "../locales/en/my-issues.json";
import type search from "../locales/en/search.json";
import type inbox from "../locales/en/inbox.json";
import type workspace from "../locales/en/workspace.json";
import type projects from "../locales/en/projects.json";
import type autopilots from "../locales/en/autopilots.json";
import type skills from "../locales/en/skills.json";
import type chat from "../locales/en/chat.json";
import type modals from "../locales/en/modals.json";
import type runtimes from "../locales/en/runtimes.json";
import type layout from "../locales/en/layout.json";
import type usage from "../locales/en/usage.json";
import type squads from "../locales/en/squads.json";

// Module augmentation enables i18next v26 selector API across the monorepo:
// `t($ => $.signin.title)` resolves to the value in en/auth.json.
// Apps don't need to redeclare this — the augmentation is global, pulled
// into the compilation graph by `use-t.ts`'s side-effect import.
//
// Adding a namespace: drop a JSON file under en/ and zh-Hans/, then add
// the matching `import type` + entry below. Type inference on `t($ => $)`
// follows automatically.
//
// The resource shape lives on a global `I18nResources` interface (not a
// type literal inside CustomTypeOptions) so other packages can contribute
// namespaces via declaration merging. See packages/ui/types/i18next.d.ts —
// it adds the `ui` namespace there, which lets packages/ui typecheck the
// selector form standalone without depending on @multica/views.
declare global {
  interface I18nResources {
    common: typeof common;
    auth: typeof auth;
    settings: typeof settings;
    issues: typeof issues;
    agents: typeof agents;
    editor: typeof editor;
    onboarding: typeof onboarding;
    invite: typeof invite;
    labels: typeof labels;
    members: typeof members;
    "my-issues": typeof myIssues;
    search: typeof search;
    inbox: typeof inbox;
    workspace: typeof workspace;
    projects: typeof projects;
    autopilots: typeof autopilots;
    skills: typeof skills;
    chat: typeof chat;
    modals: typeof modals;
    runtimes: typeof runtimes;
    layout: typeof layout;
    usage: typeof usage;
    squads: typeof squads;
  }
}

declare module "i18next" {
  interface CustomTypeOptions {
    defaultNS: "common";
    resources: I18nResources;
    enableSelector: true;
  }
}
