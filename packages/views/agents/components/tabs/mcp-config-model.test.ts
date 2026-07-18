import { describe, expect, it } from "vitest";
import {
  listManagedMcpServers,
  removeManagedMcpServer,
  upsertManagedMcpServer,
} from "./mcp-config-model";

describe("mcp config compatibility model", () => {
  it("reads canonical and historical OpenCode-native containers without duplication", () => {
    const servers = listManagedMcpServers({
      mcpServers: { shared: { command: "canonical" } },
      mcp: {
        shared: { type: "local", command: ["native"] },
        legacy: { type: "remote", url: "https://example.test/mcp" },
      },
    });

    expect(servers.map(({ name, container }) => ({ name, container }))).toEqual([
      { name: "legacy", container: "mcp" },
      { name: "shared", container: "mcpServers" },
    ]);
  });

  it("updates a single legacy entry while preserving unknown document fields", () => {
    const value = {
      provider: "opencode",
      mcp: {
        legacy: { type: "remote", url: "https://old.test/mcp" },
        sibling: { type: "local", command: ["node", "server.js"] },
      },
    };
    const legacy = listManagedMcpServers(value).find(
      (server) => server.name === "legacy",
    );
    expect(legacy).toBeDefined();

    expect(
      upsertManagedMcpServer(
        value,
        legacy!,
        "legacy",
        { type: "remote", url: "https://new.test/mcp" },
      ),
    ).toEqual({
      provider: "opencode",
      mcp: {
        legacy: { type: "remote", url: "https://new.test/mcp" },
        sibling: { type: "local", command: ["node", "server.js"] },
      },
    });
  });

  it("returns null only when deleting the last value from an otherwise empty document", () => {
    const value = { mcpServers: { fetch: { command: "uvx" } } };
    const [fetch] = listManagedMcpServers(value);
    expect(removeManagedMcpServer(value, fetch!)).toBeNull();

    const withMetadata = { version: 1, ...value };
    const [withMetadataFetch] = listManagedMcpServers(withMetadata);
    expect(removeManagedMcpServer(withMetadata, withMetadataFetch!)).toEqual({
      version: 1,
    });
  });
});
