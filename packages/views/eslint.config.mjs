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
  "autopilots/components/autopilots-page.tsx",
  "autopilots/components/autopilot-detail-page.tsx",
  "autopilots/components/autopilot-dialog.tsx",
  "autopilots/components/trigger-config.tsx",
  "autopilots/components/pickers/agent-picker.tsx",
  "autopilots/components/pickers/timezone-picker.tsx",
  "skills/components/skills-page.tsx",
  "skills/components/skill-detail-page.tsx",
  "skills/components/skill-columns.tsx",
  "skills/components/create-skill-dialog.tsx",
  "skills/components/runtime-local-skill-import-panel.tsx",
  "skills/components/file-tree.tsx",
  "skills/components/file-viewer.tsx",
  "chat/components/chat-fab.tsx",
  "chat/components/chat-input.tsx",
  "chat/components/chat-message-list.tsx",
  "chat/components/chat-session-history.tsx",
  "chat/components/chat-window.tsx",
  "chat/components/context-anchor.tsx",
  "chat/components/no-agent-banner.tsx",
  "chat/components/offline-banner.tsx",
  "chat/components/task-status-pill.tsx",
  "modals/backlog-agent-hint.tsx",
  "modals/set-parent-issue.tsx",
  "modals/add-child-issue.tsx",
  "modals/delete-issue-confirm.tsx",
  "modals/feedback.tsx",
  "modals/issue-picker-modal.tsx",
  "modals/create-workspace.tsx",
  "modals/create-project.tsx",
  "modals/create-issue.tsx",
  "modals/quick-create-issue.tsx",
  "settings/components/settings-page.tsx",
  "settings/components/account-tab.tsx",
  "settings/components/notifications-tab.tsx",
  "settings/components/labs-tab.tsx",
  "settings/components/repositories-tab.tsx",
  "settings/components/tokens-tab.tsx",
  "settings/components/workspace-tab.tsx",
  "settings/components/members-tab.tsx",
  "settings/components/delete-workspace-dialog.tsx",
  "runtimes/components/runtimes-page.tsx",
  "runtimes/components/runtime-detail-page.tsx",
  "runtimes/components/runtime-detail.tsx",
  "runtimes/components/shared.tsx",
  "layout/app-sidebar.tsx",
  "layout/help-launcher.tsx",
  "layout/workspace-loader.tsx",
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
