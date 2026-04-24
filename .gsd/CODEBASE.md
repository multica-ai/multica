# Codebase Map

Generated: 2026-04-22T18:25:10Z | Files: 500 | Described: 0/500
<!-- gsd:codebase-meta {"generatedAt":"2026-04-22T18:25:10Z","fingerprint":"000a2ef3dc6d72d7955933e0eb681d3844bbfef4","fileCount":500,"truncated":true} -->
Note: Truncated to first 500 files. Run with higher --max-files to include all.

### (root)/
- *(25 files: 11 .md, 8 (no ext), 3 .yml, 1 .example, 1 .web, 1 .json)*

### .github/
- `.github/PULL_REQUEST_TEMPLATE.md`

### .github/ISSUE_TEMPLATE/
- `.github/ISSUE_TEMPLATE/bug_report.yml`
- `.github/ISSUE_TEMPLATE/config.yml`
- `.github/ISSUE_TEMPLATE/feature_request.yml`

### .github/workflows/
- `.github/workflows/ci.yml`
- `.github/workflows/desktop-smoke.yml`
- `.github/workflows/release.yml`

### apps/desktop/
- `apps/desktop/.env.production`
- `apps/desktop/.gitignore`
- `apps/desktop/electron-builder.yml`
- `apps/desktop/electron.vite.config.ts`
- `apps/desktop/eslint.config.mjs`
- `apps/desktop/package.json`
- `apps/desktop/tsconfig.json`
- `apps/desktop/tsconfig.node.json`
- `apps/desktop/tsconfig.web.json`
- `apps/desktop/vitest.config.ts`

### apps/desktop/scripts/
- `apps/desktop/scripts/brand-dev-electron.mjs`
- `apps/desktop/scripts/bundle-cli.mjs`
- `apps/desktop/scripts/package.mjs`
- `apps/desktop/scripts/package.test.mjs`

### apps/desktop/src/main/
- `apps/desktop/src/main/cli-bootstrap.ts`
- `apps/desktop/src/main/cli-release-asset.test.ts`
- `apps/desktop/src/main/cli-release-asset.ts`
- `apps/desktop/src/main/daemon-manager.ts`
- `apps/desktop/src/main/external-url.test.ts`
- `apps/desktop/src/main/external-url.ts`
- `apps/desktop/src/main/index.ts`
- `apps/desktop/src/main/updater.ts`
- `apps/desktop/src/main/version-decision.test.ts`
- `apps/desktop/src/main/version-decision.ts`

### apps/desktop/src/preload/
- `apps/desktop/src/preload/index.d.ts`
- `apps/desktop/src/preload/index.ts`

### apps/desktop/src/renderer/
- `apps/desktop/src/renderer/index.html`

### apps/desktop/src/renderer/src/
- `apps/desktop/src/renderer/src/App.tsx`
- `apps/desktop/src/renderer/src/env.d.ts`
- `apps/desktop/src/renderer/src/globals.css`
- `apps/desktop/src/renderer/src/main.tsx`
- `apps/desktop/src/renderer/src/routes.tsx`

### apps/desktop/src/renderer/src/components/
- `apps/desktop/src/renderer/src/components/daemon-panel.tsx`
- `apps/desktop/src/renderer/src/components/daemon-runtime-card.tsx`
- `apps/desktop/src/renderer/src/components/daemon-settings-tab.tsx`
- `apps/desktop/src/renderer/src/components/desktop-layout.tsx`
- `apps/desktop/src/renderer/src/components/tab-bar.tsx`
- `apps/desktop/src/renderer/src/components/tab-content.tsx`
- `apps/desktop/src/renderer/src/components/update-notification.tsx`
- `apps/desktop/src/renderer/src/components/updates-settings-tab.tsx`
- `apps/desktop/src/renderer/src/components/window-overlay.tsx`
- `apps/desktop/src/renderer/src/components/workspace-route-layout.tsx`

### apps/desktop/src/renderer/src/hooks/
- `apps/desktop/src/renderer/src/hooks/use-document-title.ts`
- `apps/desktop/src/renderer/src/hooks/use-tab-history.ts`
- `apps/desktop/src/renderer/src/hooks/use-tab-router-sync.ts`
- `apps/desktop/src/renderer/src/hooks/use-tab-sync.ts`

### apps/desktop/src/renderer/src/pages/
- `apps/desktop/src/renderer/src/pages/autopilot-detail-page.tsx`
- `apps/desktop/src/renderer/src/pages/issue-detail-page.tsx`
- `apps/desktop/src/renderer/src/pages/login.tsx`
- `apps/desktop/src/renderer/src/pages/project-detail-page.tsx`

### apps/desktop/src/renderer/src/platform/
- `apps/desktop/src/renderer/src/platform/navigation.tsx`

### apps/desktop/src/renderer/src/stores/
- `apps/desktop/src/renderer/src/stores/tab-store.test.ts`
- `apps/desktop/src/renderer/src/stores/tab-store.ts`
- `apps/desktop/src/renderer/src/stores/window-overlay-store.ts`

### apps/desktop/src/shared/
- `apps/desktop/src/shared/daemon-types.ts`

### apps/desktop/test/
- `apps/desktop/test/setup.ts`

### apps/docs/
- `apps/docs/.gitignore`
- `apps/docs/next-env.d.ts`
- `apps/docs/next.config.mjs`
- `apps/docs/package.json`
- `apps/docs/postcss.config.mjs`
- `apps/docs/source.config.ts`
- `apps/docs/tsconfig.json`

### apps/docs/app/
- `apps/docs/app/global.css`
- `apps/docs/app/layout.config.tsx`
- `apps/docs/app/layout.tsx`
- `apps/docs/app/not-found.tsx`
- `apps/docs/app/page.tsx`

### apps/docs/app/[...slug]/
- `apps/docs/app/[...slug]/page.tsx`

### apps/docs/app/api/search/
- `apps/docs/app/api/search/route.ts`

### apps/docs/content/docs/
- `apps/docs/content/docs/index.mdx`
- `apps/docs/content/docs/meta.json`

### apps/docs/content/docs/cli/
- `apps/docs/content/docs/cli/installation.mdx`
- `apps/docs/content/docs/cli/meta.json`
- `apps/docs/content/docs/cli/reference.mdx`

### apps/docs/content/docs/developers/
- `apps/docs/content/docs/developers/architecture.mdx`
- `apps/docs/content/docs/developers/contributing.mdx`
- `apps/docs/content/docs/developers/meta.json`

### apps/docs/content/docs/getting-started/
- `apps/docs/content/docs/getting-started/cloud-quickstart.mdx`
- `apps/docs/content/docs/getting-started/meta.json`
- `apps/docs/content/docs/getting-started/self-hosting.mdx`

### apps/docs/content/docs/guides/
- `apps/docs/content/docs/guides/agents.mdx`
- `apps/docs/content/docs/guides/meta.json`
- `apps/docs/content/docs/guides/quickstart.mdx`

### apps/docs/lib/
- `apps/docs/lib/source.ts`

### apps/web/
- `apps/web/components.json`
- `apps/web/eslint.config.mjs`
- `apps/web/next-env.d.ts`
- `apps/web/next.config.ts`
- `apps/web/package.json`
- `apps/web/postcss.config.mjs`
- `apps/web/proxy.ts`
- `apps/web/tsconfig.json`
- `apps/web/vitest.config.ts`

### apps/web/app/
- `apps/web/app/custom.css`
- `apps/web/app/globals.css`
- `apps/web/app/layout.tsx`
- `apps/web/app/robots.ts`
- `apps/web/app/sitemap.ts`

### apps/web/app/(auth)/invite/[id]/
- `apps/web/app/(auth)/invite/[id]/page.tsx`

### apps/web/app/(auth)/login/
- `apps/web/app/(auth)/login/page.test.tsx`
- `apps/web/app/(auth)/login/page.tsx`

### apps/web/app/(auth)/onboarding/
- `apps/web/app/(auth)/onboarding/page.tsx`

### apps/web/app/(auth)/workspaces/new/
- `apps/web/app/(auth)/workspaces/new/page.tsx`

### apps/web/app/(landing)/
- `apps/web/app/(landing)/layout.tsx`
- `apps/web/app/(landing)/page.tsx`

### apps/web/app/(landing)/about/
- `apps/web/app/(landing)/about/page.tsx`

### apps/web/app/(landing)/changelog/
- `apps/web/app/(landing)/changelog/page.tsx`

### apps/web/app/(landing)/homepage/
- `apps/web/app/(landing)/homepage/page.tsx`

### apps/web/app/[workspaceSlug]/
- `apps/web/app/[workspaceSlug]/layout.tsx`

### apps/web/app/[workspaceSlug]/(dashboard)/
- `apps/web/app/[workspaceSlug]/(dashboard)/layout.tsx`

### apps/web/app/[workspaceSlug]/(dashboard)/agents/
- `apps/web/app/[workspaceSlug]/(dashboard)/agents/page.tsx`

### apps/web/app/[workspaceSlug]/(dashboard)/autopilots/
- `apps/web/app/[workspaceSlug]/(dashboard)/autopilots/page.tsx`

### apps/web/app/[workspaceSlug]/(dashboard)/autopilots/[id]/
- `apps/web/app/[workspaceSlug]/(dashboard)/autopilots/[id]/page.tsx`

### apps/web/app/[workspaceSlug]/(dashboard)/inbox/
- `apps/web/app/[workspaceSlug]/(dashboard)/inbox/page.tsx`

### apps/web/app/[workspaceSlug]/(dashboard)/issues/
- `apps/web/app/[workspaceSlug]/(dashboard)/issues/page.tsx`

### apps/web/app/[workspaceSlug]/(dashboard)/issues/[id]/
- `apps/web/app/[workspaceSlug]/(dashboard)/issues/[id]/page.tsx`

### apps/web/app/[workspaceSlug]/(dashboard)/my-issues/
- `apps/web/app/[workspaceSlug]/(dashboard)/my-issues/page.tsx`

### apps/web/app/[workspaceSlug]/(dashboard)/projects/
- `apps/web/app/[workspaceSlug]/(dashboard)/projects/page.tsx`

### apps/web/app/[workspaceSlug]/(dashboard)/projects/[id]/
- `apps/web/app/[workspaceSlug]/(dashboard)/projects/[id]/page.tsx`

### apps/web/app/[workspaceSlug]/(dashboard)/runtimes/
- `apps/web/app/[workspaceSlug]/(dashboard)/runtimes/page.tsx`

### apps/web/app/[workspaceSlug]/(dashboard)/settings/
- `apps/web/app/[workspaceSlug]/(dashboard)/settings/page.tsx`

### apps/web/app/[workspaceSlug]/(dashboard)/skills/
- `apps/web/app/[workspaceSlug]/(dashboard)/skills/page.tsx`

### apps/web/app/auth/callback/
- `apps/web/app/auth/callback/page.test.tsx`
- `apps/web/app/auth/callback/page.tsx`

### apps/web/app/favicon.ico/
- `apps/web/app/favicon.ico/route.ts`

### apps/web/components/
- `apps/web/components/locale-sync.tsx`
- `apps/web/components/pageview-tracker.tsx`
- `apps/web/components/theme-provider.tsx`
- `apps/web/components/web-providers.tsx`

### apps/web/features/auth/
- `apps/web/features/auth/auth-cookie.ts`

### apps/web/features/landing/components/
- `apps/web/features/landing/components/about-page-client.tsx`
- `apps/web/features/landing/components/changelog-page-client.tsx`
- `apps/web/features/landing/components/faq-section.tsx`
- `apps/web/features/landing/components/features-section.tsx`
- `apps/web/features/landing/components/how-it-works-section.tsx`
- `apps/web/features/landing/components/landing-footer.tsx`
- `apps/web/features/landing/components/landing-header.tsx`
- `apps/web/features/landing/components/landing-hero.tsx`
- `apps/web/features/landing/components/multica-landing.tsx`
- `apps/web/features/landing/components/open-source-section.tsx`
- `apps/web/features/landing/components/redirect-if-authenticated.tsx`
- `apps/web/features/landing/components/shared.tsx`

### apps/web/features/landing/i18n/
- `apps/web/features/landing/i18n/context.tsx`
- `apps/web/features/landing/i18n/en.ts`
- `apps/web/features/landing/i18n/index.ts`
- `apps/web/features/landing/i18n/types.ts`
- `apps/web/features/landing/i18n/zh.ts`

### apps/web/platform/
- `apps/web/platform/navigation.tsx`

### apps/web/test/
- `apps/web/test/helpers.tsx`
- `apps/web/test/setup.ts`

### docker/
- `docker/entrypoint.sh`

### docs/
- `docs/analytics.md`
- `docs/codex-sandbox-troubleshooting.md`
- `docs/design.md`
- `docs/onboarding-redesign-proposal.md`
- `docs/product-overview.md`
- `docs/workspace-url-refactor-proposal.md`

### docs/plans/
- `docs/plans/2026-04-07-tanstack-query-migration.md`
- `docs/plans/2026-04-08-board-dnd-rewrite.md`
- `docs/plans/2026-04-08-drag-upload-enhancement.md`
- `docs/plans/2026-04-08-image-view-enhancement.md`
- `docs/plans/2026-04-08-monorepo-extraction.md`
- `docs/plans/2026-04-09-desktop-app.md`
- `docs/plans/2026-04-09-monorepo-extraction.md`
- `docs/plans/2026-04-09-upload-attachment-fixes.md`
- `docs/plans/2026-04-15-workspace-slug-url-refactor.md`
- `docs/plans/2026-04-16-remove-onboarding-and-fix-daemon-bootstrap.md`
- `docs/plans/2026-04-16-unify-workspace-identity-resolver.md`

### e2e/
- `e2e/auth.spec.ts`
- `e2e/comments.spec.ts`
- `e2e/env.ts`
- `e2e/fixtures.ts`
- `e2e/helpers.ts`
- `e2e/issues.spec.ts`
- `e2e/navigation.spec.ts`
- `e2e/settings.spec.ts`

### packages/core/
- `packages/core/eslint.config.mjs`
- `packages/core/hooks.tsx`
- `packages/core/index.ts`
- `packages/core/logger.ts`
- `packages/core/package.json`
- `packages/core/provider.tsx`
- `packages/core/query-client.ts`
- `packages/core/tsconfig.json`
- `packages/core/utils.test.ts`
- `packages/core/utils.ts`
- `packages/core/vitest.config.ts`

### packages/core/analytics/
- `packages/core/analytics/index.ts`

### packages/core/api/
- `packages/core/api/client.test.ts`
- `packages/core/api/client.ts`
- `packages/core/api/index.ts`
- `packages/core/api/ws-client.ts`

### packages/core/auth/
- `packages/core/auth/index.ts`
- `packages/core/auth/store.test.ts`
- `packages/core/auth/store.ts`
- `packages/core/auth/utils.test.ts`
- `packages/core/auth/utils.ts`

### packages/core/autopilots/
- `packages/core/autopilots/index.ts`
- `packages/core/autopilots/mutations.ts`
- `packages/core/autopilots/queries.ts`

### packages/core/chat/
- `packages/core/chat/index.ts`
- `packages/core/chat/mutations.ts`
- `packages/core/chat/queries.ts`
- `packages/core/chat/store.ts`

### packages/core/config/
- `packages/core/config/index.ts`

### packages/core/constants/
- `packages/core/constants/upload.ts`

### packages/core/hooks/
- `packages/core/hooks/use-file-upload.ts`

### packages/core/inbox/
- `packages/core/inbox/index.ts`
- `packages/core/inbox/mutations.ts`
- `packages/core/inbox/queries.ts`
- `packages/core/inbox/ws-updaters.test.ts`
- `packages/core/inbox/ws-updaters.ts`

### packages/core/issues/
- `packages/core/issues/cache-helpers.ts`
- `packages/core/issues/column-config.ts`
- `packages/core/issues/index.ts`
- `packages/core/issues/mutations.ts`
- `packages/core/issues/queries.ts`
- `packages/core/issues/store.ts`
- `packages/core/issues/ws-updaters.ts`

### packages/core/issues/config/
- `packages/core/issues/config/index.ts`
- `packages/core/issues/config/priority.ts`
- `packages/core/issues/config/status.ts`

### packages/core/issues/stores/
- `packages/core/issues/stores/comment-collapse-store.ts`
- `packages/core/issues/stores/draft-store.ts`
- `packages/core/issues/stores/index.ts`
- `packages/core/issues/stores/issues-scope-store.ts`
- `packages/core/issues/stores/my-issues-view-store.ts`
- `packages/core/issues/stores/recent-issues-store.ts`
- `packages/core/issues/stores/selection-store.ts`
- `packages/core/issues/stores/view-store-context.tsx`
- `packages/core/issues/stores/view-store.ts`

### packages/core/modals/
- `packages/core/modals/index.ts`
- `packages/core/modals/store.ts`

### packages/core/navigation/
- `packages/core/navigation/index.ts`
- `packages/core/navigation/store.test.ts`
- `packages/core/navigation/store.ts`

### packages/core/onboarding/
- `packages/core/onboarding/index.ts`
- `packages/core/onboarding/recommend-template.test.ts`
- `packages/core/onboarding/recommend-template.ts`
- `packages/core/onboarding/step-order.ts`
- `packages/core/onboarding/store.ts`
- `packages/core/onboarding/types.ts`

### packages/core/paths/
- `packages/core/paths/consistency.test.ts`
- `packages/core/paths/hooks.tsx`
- `packages/core/paths/index.ts`
- `packages/core/paths/paths.test.ts`
- `packages/core/paths/paths.ts`
- `packages/core/paths/reserved-slugs.ts`
- `packages/core/paths/resolve.test.ts`
- `packages/core/paths/resolve.ts`

### packages/core/pins/
- `packages/core/pins/index.ts`
- `packages/core/pins/mutations.ts`
- `packages/core/pins/queries.ts`

### packages/core/pipeline/
- `packages/core/pipeline/index.ts`

### packages/core/platform/
- `packages/core/platform/auth-initializer.tsx`
- `packages/core/platform/core-provider.tsx`
- `packages/core/platform/index.ts`
- `packages/core/platform/persist-storage.test.ts`
- `packages/core/platform/persist-storage.ts`
- `packages/core/platform/storage-cleanup.test.ts`
- `packages/core/platform/storage-cleanup.ts`
- `packages/core/platform/storage.ts`
- `packages/core/platform/types.ts`
- `packages/core/platform/workspace-storage.test.ts`
- `packages/core/platform/workspace-storage.ts`

### packages/core/projects/
- `packages/core/projects/config.ts`
- `packages/core/projects/index.ts`
- `packages/core/projects/mutations.ts`
- `packages/core/projects/queries.ts`

### packages/core/realtime/
- `packages/core/realtime/hooks.ts`
- `packages/core/realtime/index.ts`
- `packages/core/realtime/provider.tsx`
- `packages/core/realtime/use-realtime-sync.ts`

### packages/core/runtimes/
- `packages/core/runtimes/hooks.ts`
- `packages/core/runtimes/index.ts`
- `packages/core/runtimes/models.ts`
- `packages/core/runtimes/mutations.ts`
- `packages/core/runtimes/queries.ts`

### packages/core/types/
- `packages/core/types/activity.ts`
- `packages/core/types/agent.ts`
- `packages/core/types/api.ts`
- `packages/core/types/attachment.ts`
- `packages/core/types/autopilot.ts`
- `packages/core/types/chat.ts`
- `packages/core/types/column-config.ts`
- `packages/core/types/comment.ts`
- `packages/core/types/events.ts`
- `packages/core/types/inbox.ts`
- `packages/core/types/index.ts`
- `packages/core/types/issue.ts`
- `packages/core/types/pin.ts`
- `packages/core/types/pipeline.ts`
- `packages/core/types/project.ts`
- `packages/core/types/storage.ts`
- `packages/core/types/subscriber.ts`
- `packages/core/types/workspace.ts`

### packages/core/workspace/
- `packages/core/workspace/hooks.ts`
- `packages/core/workspace/index.ts`
- `packages/core/workspace/mutations.ts`
- `packages/core/workspace/queries.ts`

### packages/eslint-config/
- `packages/eslint-config/base.js`
- `packages/eslint-config/next.js`
- `packages/eslint-config/package.json`
- `packages/eslint-config/react.js`

### packages/tsconfig/
- `packages/tsconfig/base.json`
- `packages/tsconfig/package.json`
- `packages/tsconfig/react-library.json`

### packages/ui/
- `packages/ui/components.json`
- `packages/ui/eslint.config.mjs`
- `packages/ui/package.json`
- `packages/ui/tsconfig.json`

### packages/ui/components/common/
- `packages/ui/components/common/actor-avatar.tsx`
- `packages/ui/components/common/emoji-picker.tsx`
- `packages/ui/components/common/file-upload-button.tsx`
- `packages/ui/components/common/mention-hover-card.tsx`
- `packages/ui/components/common/multica-icon.tsx`
- `packages/ui/components/common/quick-emoji-picker.tsx`
- `packages/ui/components/common/reaction-bar.tsx`
- `packages/ui/components/common/submit-button.tsx`
- `packages/ui/components/common/theme-provider.tsx`

### packages/ui/components/ui/
- *(55 files: 55 .tsx)*

### packages/ui/hooks/
- `packages/ui/hooks/use-auto-scroll.ts`
- `packages/ui/hooks/use-mobile.ts`
- `packages/ui/hooks/use-scroll-fade.ts`

### packages/ui/lib/
- `packages/ui/lib/utils.ts`

### packages/ui/markdown/
- `packages/ui/markdown/CodeBlock.tsx`
- `packages/ui/markdown/file-cards.ts`
- `packages/ui/markdown/index.ts`
- `packages/ui/markdown/linkify.ts`
- `packages/ui/markdown/Markdown.tsx`
- `packages/ui/markdown/mentions.ts`
- `packages/ui/markdown/StreamingMarkdown.tsx`

### packages/ui/styles/
- `packages/ui/styles/base.css`
- `packages/ui/styles/tokens.css`

### packages/views/
- `packages/views/eslint.config.mjs`

### packages/views/agents/
- `packages/views/agents/config.ts`
- `packages/views/agents/index.ts`

### packages/views/agents/components/
- `packages/views/agents/components/agent-detail.tsx`
- `packages/views/agents/components/agent-list-item.tsx`
- `packages/views/agents/components/agents-page.tsx`
- `packages/views/agents/components/create-agent-dialog.tsx`
- `packages/views/agents/components/index.ts`
- `packages/views/agents/components/model-dropdown.tsx`

### packages/views/agents/components/tabs/
- `packages/views/agents/components/tabs/custom-args-tab.tsx`
- `packages/views/agents/components/tabs/env-tab.tsx`
- `packages/views/agents/components/tabs/instructions-tab.tsx`
- `packages/views/agents/components/tabs/settings-tab.tsx`
- `packages/views/agents/components/tabs/skills-tab.tsx`
- `packages/views/agents/components/tabs/tasks-tab.test.tsx`
- `packages/views/agents/components/tabs/tasks-tab.tsx`

### packages/views/auth/
- `packages/views/auth/index.ts`
- `packages/views/auth/login-page.test.tsx`
- `packages/views/auth/login-page.tsx`
- `packages/views/auth/use-logout.ts`

### packages/views/autopilots/components/
- `packages/views/autopilots/components/autopilot-detail-page.tsx`
- `packages/views/autopilots/components/autopilots-page.tsx`
- `packages/views/autopilots/components/index.ts`
- `packages/views/autopilots/components/trigger-config.tsx`

### packages/views/chat/
- `packages/views/chat/index.ts`

### packages/views/chat/components/
- `packages/views/chat/components/chat-fab.tsx`
- `packages/views/chat/components/chat-input.tsx`
- `packages/views/chat/components/chat-message-list.tsx`
- `packages/views/chat/components/chat-resize-handles.tsx`
- `packages/views/chat/components/chat-session-history.tsx`
- `packages/views/chat/components/chat-window.tsx`
- `packages/views/chat/components/use-chat-resize.ts`

### packages/views/common/
- `packages/views/common/actor-avatar.tsx`
- `packages/views/common/markdown.tsx`

### packages/views/editor/
- `packages/views/editor/bubble-menu.tsx`
- `packages/views/editor/content-editor.css`
- `packages/views/editor/content-editor.test.tsx`
- `packages/views/editor/content-editor.tsx`
- `packages/views/editor/file-drop-overlay.tsx`
- `packages/views/editor/index.ts`
- `packages/views/editor/link-hover-card.tsx`
- `packages/views/editor/readonly-content.tsx`
- `packages/views/editor/title-editor.css`
- `packages/views/editor/title-editor.tsx`
- `packages/views/editor/use-file-drop-zone.ts`

### packages/views/editor/extensions/
- `packages/views/editor/extensions/blur-shortcut.ts`
- `packages/views/editor/extensions/code-block-view.tsx`
- `packages/views/editor/extensions/file-card.tsx`
- `packages/views/editor/extensions/file-upload.ts`
- `packages/views/editor/extensions/image-view.tsx`
- `packages/views/editor/extensions/index.ts`
- `packages/views/editor/extensions/markdown-paste.ts`
- `packages/views/editor/extensions/mention-extension.ts`
- `packages/views/editor/extensions/mention-suggestion.test.tsx`
- `packages/views/editor/extensions/mention-suggestion.tsx`
- `packages/views/editor/extensions/mention-view.tsx`
- `packages/views/editor/extensions/submit-shortcut.ts`

### packages/views/editor/utils/
- `packages/views/editor/utils/clipboard.ts`
- `packages/views/editor/utils/link-handler.ts`
- `packages/views/editor/utils/preprocess.ts`

### packages/views/inbox/
- `packages/views/inbox/index.ts`

### packages/views/inbox/components/
- `packages/views/inbox/components/inbox-detail-label.tsx`
- `packages/views/inbox/components/inbox-list-item.tsx`
- `packages/views/inbox/components/inbox-page.tsx`
- `packages/views/inbox/components/index.ts`

### packages/views/invite/
- `packages/views/invite/index.ts`
- `packages/views/invite/invite-page.tsx`

### packages/views/issues/components/
- `packages/views/issues/components/agent-live-card.tsx`
- `packages/views/issues/components/agent-transcript-dialog.tsx`
- `packages/views/issues/components/backlog-agent-hint-dialog.tsx`
- `packages/views/issues/components/batch-action-toolbar.tsx`
- `packages/views/issues/components/board-card.tsx`
- `packages/views/issues/components/board-column.tsx`
- `packages/views/issues/components/board-view.tsx`
- `packages/views/issues/components/column-instructions-modal.tsx`
- `packages/views/issues/components/comment-card.tsx`
- `packages/views/issues/components/comment-input.tsx`
- `packages/views/issues/components/index.ts`
- `packages/views/issues/components/infinite-scroll-sentinel.tsx`
