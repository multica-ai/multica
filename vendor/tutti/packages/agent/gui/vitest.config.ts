import { fileURLToPath } from "node:url";
import { configDefaults, defineConfig } from "vitest/config";

const rootDir = fileURLToPath(new URL(".", import.meta.url));
const domTypeScriptTests = [
  "agent-gui/agentGuiNode/agentGuiHeroCarouselScene.spec.ts",
  "agent-gui/agentGuiNode/agentGuiNodeViewConversation.spec.ts",
  "agent-gui/agentGuiNode/agentRichText/agentFileMentionExtension.spec.ts",
  "agent-gui/agentGuiNode/composer/composerPortalTarget.spec.ts",
  "agent-gui/agentGuiNode/controller/useAgentGUIContinueConversation.spec.ts",
  "agent-gui/agentGuiNode/controller/useAgentGUIConversationSelectionController.spec.ts",
  "agent-gui/agentGuiNode/controller/useAgentGUISubmitInteractionActions.spec.ts",
  "agent-gui/agentGuiNode/model/agentGuiComposerGate.spec.ts",
  "build/cssSafeSvgDataUrl.spec.ts",
  "shared/agentConversation/lib/copyImageToClipboard.spec.ts",
  "workbench/contribution.test.ts",
  "workbench/commands.spec.ts"
];

export default defineConfig({
  resolve: {
    alias: {
      "@tutti-os/workspace-file-manager/assets/workspace-archive-fallback.png": `${rootDir}../../workspace/file-manager/src/runtime-assets/workspace-archive-fallback-url.ts`,
      "@tutti-os/workspace-file-manager/assets/workspace-folder-fallback.png": `${rootDir}../../workspace/file-manager/src/runtime-assets/workspace-folder-fallback-url.ts`,
      "@tutti-os/workspace-file-manager/services": `${rootDir}../../workspace/file-manager/src/services/index.ts`,
      "@tutti-os/workspace-file-manager": `${rootDir}../../workspace/file-manager/src/index.ts`
    }
  },
  test: {
    maxWorkers: 4,
    pool: "threads",
    projects: [
      {
        extends: true,
        test: {
          environment: "node",
          exclude: [...configDefaults.exclude, ...domTypeScriptTests],
          include: ["**/*.{spec,test}.ts"],
          name: "node",
          setupFiles: ["./vitest.shared.setup.ts"]
        }
      },
      {
        extends: true,
        test: {
          environment: "jsdom",
          include: ["**/*.{spec,test}.tsx", ...domTypeScriptTests],
          name: "dom",
          setupFiles: ["./vitest.shared.setup.ts", "./vitest.setup.ts"]
        }
      }
    ]
  }
});
