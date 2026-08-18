import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient, ApiError } from "../api/client";
import { setCurrentWorkspace } from "../platform/workspace-storage";
import {
  PLATFORM_EXTENSION_MAX_IMPORT_BYTES,
  PlatformExtensionDocumentTooLargeError,
} from "./types";

const bytes = (value: string) => new TextEncoder().encode(value);

const mapping = {
  release: {
    id: "11111111-1111-4111-8111-111111111111",
    extension_key: "research-team",
    version: "1.0.0",
    digest: `sha256:${"a".repeat(64)}`,
  },
  runtime: {
    id: "22222222-2222-4222-8222-222222222222",
    provider: "platform-agent-cli",
    name: "Platform Agent CLI",
  },
  squad: {
    id: "33333333-3333-4333-8333-333333333333",
    name: "Research Team v1.0.0",
  },
  agents: [],
  skills: [],
};

afterEach(() => {
  vi.unstubAllGlobals();
  setCurrentWorkspace(null, null);
});

describe("ApiClient platform Extensions", () => {
  it("uses the shared authenticated request path for list/detail/import, mapping saves, and version archive", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(new Response(JSON.stringify([mapping]), { status: 200 }))
      .mockResolvedValueOnce(
        new Response(JSON.stringify({ ...mapping, manifest: {} }), { status: 200 }),
      )
      .mockResolvedValueOnce(
        new Response(JSON.stringify({ ...mapping, idempotent: false }), { status: 201 }),
      )
      .mockResolvedValueOnce(
        new Response(JSON.stringify(mapping), { status: 200 }),
      )
      .mockResolvedValueOnce(
        new Response(JSON.stringify(mapping), { status: 200 }),
      );
    vi.stubGlobal("fetch", fetchMock);
    vi.stubGlobal("document", { cookie: "multica_csrf=csrf-token" });
    setCurrentWorkspace("acme", "workspace-1");
    const client = new ApiClient("https://api.example.test", {
      identity: { platform: "desktop", version: "1.2.3", os: "darwin" },
    });
    client.setToken("secret-token");

    await client.listPlatformExtensions();
    await client.getPlatformExtension(mapping.release.id);
    await client.importPlatformExtension(
      new Uint8Array([0x50, 0x4b, 0x03, 0x04]),
    );
    await client.updatePlatformExtension(mapping.release.id, {
      squad_base_name: "delegate",
      agent_runtime_ids: { leader: "" },
    });
    await client.archivePlatformExtension(mapping.release.id);

    expect(fetchMock.mock.calls.map(([url]) => url)).toEqual([
      "https://api.example.test/api/extensions",
      `https://api.example.test/api/extensions/${mapping.release.id}`,
      "https://api.example.test/api/extensions/import",
      `https://api.example.test/api/extensions/${mapping.release.id}`,
      `https://api.example.test/api/extensions/${mapping.release.id}/archive`,
    ]);
    const importInit = fetchMock.mock.calls[2]?.[1] as RequestInit;
    expect(importInit).toMatchObject({
      method: "POST",
      credentials: "include",
    });
    expect(importInit.body).toEqual(new Uint8Array([0x50, 0x4b, 0x03, 0x04]));
    expect(importInit.headers).toMatchObject({
      Authorization: "Bearer secret-token",
      "X-Workspace-Slug": "acme",
      "X-Client-Platform": "desktop",
      "X-Client-Version": "1.2.3",
      "X-Client-OS": "darwin",
      "X-CSRF-Token": "csrf-token",
      "Content-Type": "application/zip",
    });
    expect((importInit.headers as Record<string, string>)["X-Request-ID"]).toEqual(
      expect.any(String),
    );
    const updateInit = fetchMock.mock.calls[3]?.[1] as RequestInit;
    expect(updateInit).toMatchObject({
      method: "PATCH",
      body: JSON.stringify({
        squad_base_name: "delegate",
        agent_runtime_ids: { leader: "" },
      }),
    });
    expect(updateInit.headers).toMatchObject({
      "Content-Type": "application/json",
      Authorization: "Bearer secret-token",
    });
    const archiveInit = fetchMock.mock.calls[4]?.[1] as RequestInit;
    expect(archiveInit).toMatchObject({ method: "POST", credentials: "include" });
  });

  it("falls back safely for malformed successful responses", async () => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValueOnce(new Response(JSON.stringify({ rows: [] }), { status: 200 }))
        .mockResolvedValueOnce(new Response(JSON.stringify({ release: null }), { status: 200 }))
        .mockResolvedValueOnce(new Response(JSON.stringify({ idempotent: "no" }), { status: 201 })),
    );
    const client = new ApiClient("https://api.example.test");

    await expect(client.listPlatformExtensions()).resolves.toEqual([]);
    await expect(client.getPlatformExtension(mapping.release.id)).resolves.toBeNull();
    await expect(client.importPlatformExtension(bytes("{}"))).resolves.toBeNull();
  });

  it("preserves structured API errors", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            error: "no idle runtime",
            code: "PLATFORM_RUNTIME_UNAVAILABLE",
          }),
          { status: 409, statusText: "Conflict" },
        ),
      ),
    );

    const error = await new ApiClient("https://api.example.test")
      .importPlatformExtension(bytes("{}"))
      .catch((caught: unknown) => caught);
    expect(error).toBeInstanceOf(ApiError);
    expect(error).toMatchObject({
      status: 409,
      body: { code: "PLATFORM_RUNTIME_UNAVAILABLE" },
    });
  });

  it("rejects packages above the upload limit before performing a request", async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    const oversized = new Uint8Array(PLATFORM_EXTENSION_MAX_IMPORT_BYTES + 1);

    await expect(
      new ApiClient("https://api.example.test").importPlatformExtension(oversized),
    ).rejects.toBeInstanceOf(PlatformExtensionDocumentTooLargeError);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("measures the package size in bytes and accepts the exact boundary", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(
        new Response(JSON.stringify({ ...mapping, idempotent: false }), { status: 201 }),
      );
    vi.stubGlobal("fetch", fetchMock);
    const client = new ApiClient("https://api.example.test");

    await expect(
      client.importPlatformExtension(new Uint8Array(PLATFORM_EXTENSION_MAX_IMPORT_BYTES)),
    ).resolves.toMatchObject({ idempotent: false });
    expect(fetchMock).toHaveBeenCalledTimes(1);

    await expect(
      client.importPlatformExtension(new Uint8Array(PLATFORM_EXTENSION_MAX_IMPORT_BYTES + 1)),
    ).rejects.toBeInstanceOf(PlatformExtensionDocumentTooLargeError);
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it("uploads binary package bytes without attempting UTF-8 decoding", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ ...mapping, idempotent: false }), { status: 201 }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(
      new ApiClient("https://api.example.test").importPlatformExtension(
        new Uint8Array([0x50, 0x4b, 0x03, 0x04, 0xff]),
      ),
    ).resolves.toMatchObject({ idempotent: false });
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });
});
