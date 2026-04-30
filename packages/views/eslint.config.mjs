import reactConfig from "@multica/eslint-config/react";
import i18next from "eslint-plugin-i18next";

// Hard-code i18n protection on files we have already translated. Adding a
// path here means: every JSX text node on the page must be passed through
// useT() — raw strings become a build error.
//
// The list grows as new pages are translated; widening it is the canonical
// signal that "this surface is translated and stays translated". We do NOT
// turn it on globally yet — most of the codebase is still hardcoded EN, and
// a global on-switch would drown CI in noise that nobody intends to fix
// today.
const TRANSLATED_FILES = [
  "auth/login-page.tsx",
  "settings/components/appearance-tab.tsx",
  "editor/bubble-menu.tsx",
  "editor/link-hover-card.tsx",
  "editor/readonly-content.tsx",
  "editor/title-editor.tsx",
  "editor/extensions/code-block-view.tsx",
  "editor/extensions/file-card.tsx",
  "editor/extensions/image-view.tsx",
  "editor/extensions/mention-suggestion.tsx",
  "invite/invite-page.tsx",
  "labels/label-chip.tsx",
  "members/member-profile-card.tsx",
  "my-issues/components/my-issues-page.tsx",
  "my-issues/components/my-issues-header.tsx",
  "search/search-command.tsx",
  "inbox/components/inbox-page.tsx",
  "inbox/components/inbox-list-item.tsx",
  "inbox/components/inbox-detail-label.tsx",
  "workspace/create-workspace-form.tsx",
  "workspace/new-workspace-page.tsx",
  "workspace/no-access-page.tsx",
  "projects/components/projects-page.tsx",
  "projects/components/project-detail.tsx",
  "projects/components/project-picker.tsx",
  "projects/components/project-chip.tsx",
];

export default [
  ...reactConfig,
  {
    files: TRANSLATED_FILES,
    plugins: { i18next },
    rules: {
      // jsx-text-only flags raw strings inside JSX children only. JSX
      // attributes (className, aria-label) and TS literals are allowed
      // through because they have legitimate non-translatable uses; we
      // catch attribute regressions during code review instead.
      "i18next/no-literal-string": [
        "error",
        { mode: "jsx-text-only" },
      ],
    },
  },
];
