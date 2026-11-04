import { readFileSync } from "node:fs";
import { resolve } from "node:path";

import { describe, expect, it } from "vitest";

import {
  agentGUIBuildEntries,
  agentGUIDtsBuildEntries,
  agentGUIDtsEntryGroups
} from "./build/agentGuiBuildEntries";

const packageManifest = JSON.parse(
  readFileSync(resolve(process.cwd(), "package.json"), "utf8")
) as {
  publishConfig: {
    exports: Record<string, unknown>;
  };
};
const packageRootSource = readFileSync(
  resolve(process.cwd(), "index.ts"),
  "utf8"
);
const declarationTsconfig = JSON.parse(
  readFileSync(resolve(process.cwd(), "tsconfig.dts.json"), "utf8")
) as { files: string[] };

describe("Agent GUI declaration build", () => {
  it("pre-emits every runtime entry exactly once", () => {
    const declarationSources = declarationTsconfig.files;
    const runtimeSources = Object.values(agentGUIBuildEntries).sort();

    expect(new Set(declarationSources).size).toBe(declarationSources.length);
    expect([...declarationSources].sort()).toEqual(runtimeSources);
    expect(Object.keys(agentGUIDtsBuildEntries).sort()).toEqual(
      Object.keys(agentGUIBuildEntries).sort()
    );
  });

  it("rolls up every pre-emitted declaration exactly once", () => {
    const groupedEntries = agentGUIDtsEntryGroups.flat();

    expect(new Set(groupedEntries).size).toBe(groupedEntries.length);
    expect([...groupedEntries].sort()).toEqual(
      Object.keys(agentGUIDtsBuildEntries).sort()
    );
  });

  it("publishes every declaration build entry", () => {
    const publishedDeclarationEntries = Object.values(
      packageManifest.publishConfig.exports
    )
      .flatMap((value) => {
        if (!value || typeof value !== "object" || !("types" in value)) {
          return [];
        }
        return [String(value.types)];
      })
      .map((path) => path.replace(/^\.\/dist\//, "").replace(/\.d\.ts$/, ""))
      .sort();

    expect(publishedDeclarationEntries).toEqual(
      Object.keys(agentGUIBuildEntries).sort()
    );
  });

  it("builds and publishes the workspace settings panel contract", () => {
    expect(agentGUIBuildEntries["workspace-settings-panel"]).toBe(
      "shared/workspaceSettingsPanel/workspaceSettingsPanelStore.ts"
    );
    expect(
      packageManifest.publishConfig.exports["./workspace-settings-panel"]
    ).toEqual({
      types: "./dist/workspace-settings-panel.d.ts",
      import: "./dist/workspace-settings-panel.js"
    });
  });

  it("builds and publishes the headless conversation message controller", () => {
    expect(agentGUIBuildEntries["conversation-message-controller"]).toBe(
      "agentConversationMessageController.ts"
    );
    expect(
      packageManifest.publishConfig.exports["./conversation-message-controller"]
    ).toEqual({
      types: "./dist/conversation-message-controller.d.ts",
      import: "./dist/conversation-message-controller.js"
    });
  });

  it("keeps the workspace query cache package-internal", () => {
    expect(agentGUIBuildEntries).not.toHaveProperty("workspace-query-cache");
    expect(
      packageManifest.publishConfig.exports["./workspace-query-cache"]
    ).toBeUndefined();
  });

  it("builds and publishes the DOM-free conversation projection", () => {
    expect(agentGUIBuildEntries["conversation-projection"]).toBe(
      "conversationProjection.ts"
    );
    expect(
      packageManifest.publishConfig.exports["./conversation-projection"]
    ).toEqual({
      types: "./dist/conversation-projection.d.ts",
      import: "./dist/conversation-projection.js"
    });
  });

  it("builds and publishes the DOM-free conversation activity projection", () => {
    expect(agentGUIBuildEntries["conversation-activity-projection"]).toBe(
      "conversation-activity-projection.ts"
    );
    expect(
      packageManifest.publishConfig.exports[
        "./conversation-activity-projection"
      ]
    ).toEqual({
      types: "./dist/conversation-activity-projection.d.ts",
      import: "./dist/conversation-activity-projection.js"
    });
  });

  it("builds and publishes the DOM-free composer projection", () => {
    expect(agentGUIBuildEntries["composer-projection"]).toBe(
      "composerProjection.ts"
    );
    expect(
      packageManifest.publishConfig.exports["./composer-projection"]
    ).toEqual({
      types: "./dist/composer-projection.d.ts",
      import: "./dist/composer-projection.js"
    });
  });
});
