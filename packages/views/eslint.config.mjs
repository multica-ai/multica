import reactConfig from "@multica/eslint-config/react";
import i18next from "eslint-plugin-i18next";

// Global i18n protection. Every JSX text node anywhere in this package
// must pass through useT() — raw strings become a build error. Files
// listed in `STILL_HARDCODED` are explicit holdouts that haven't been
// translated yet; the goal is to drain that list to zero.
//
// Scope of `mode: "jsx-text-only"`: flags raw strings inside JSX
// children only. Attribute values (className, aria-label) and plain
// TypeScript string literals are allowed through because they have
// legitimate non-translatable uses (CSS classes, framework defaults,
// dev-tool keys); attribute regressions are caught in code review.

// Files that still contain hardcoded EN strings and have NOT been wired
// to the i18n bundle yet. New files added here SHOULD also have an
// issue or follow-up commit driving them to zero.
const STILL_HARDCODED = [
  // Onboarding deep steps — flagged in the rollout plan; large surfaces
  // with copy-heavy content that benefits from a focused translation pass.
  "onboarding/steps/step-workspace.tsx",
  "onboarding/steps/step-runtime-connect.tsx",
  "onboarding/steps/step-platform-fork.tsx",
  "onboarding/steps/step-agent.tsx",
  "onboarding/steps/step-first-issue.tsx",
  "onboarding/steps/cli-install-instructions.tsx",
  "onboarding/components/runtime-aside-panel.tsx",
  "onboarding/components/starter-content-prompt.tsx",
  "onboarding/components/cloud-waitlist-expand.tsx",
  "onboarding/components/compact-runtime-row.tsx",
  // Runtimes usage panel — chart-heavy KPI / breakdown / receipt UI.
  "runtimes/components/usage-section.tsx",
  // Agents minor — sparkline label, profile card hover, presence
  // indicator chips. Visual primitives with little text but on the
  // hardcoded side until translated.
  "agents/components/sparkline.tsx",
  "agents/components/agent-presence-indicator.tsx",
  "agents/components/agent-profile-card.tsx",
  "agents/components/visibility-badge.tsx",
  "agents/components/char-counter.tsx",
  // common/ helpers — task-transcript family + a few inbox/issue
  // detail bits that read shared. They'll come along with whichever
  // namespace pulls them next.
];

export default [
  ...reactConfig,
  {
    files: ["**/*.tsx"],
    ignores: ["**/*.test.tsx", "test/**", ...STILL_HARDCODED],
    plugins: { i18next },
    rules: {
      "i18next/no-literal-string": [
        "error",
        { mode: "jsx-text-only" },
      ],
    },
  },
];
