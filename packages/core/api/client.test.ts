import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient, ApiError } from "./client";

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("ApiClient", () => {
  it("does not clear a newer token when a stale request returns 401", async () => {
    let resolveStaleRequest:
      | ((response: Response) => void)
      | undefined;
    const onUnauthorized = vi.fn();
    const fetchMock = vi
      .fn()
      .mockImplementationOnce(
        () =>
          new Promise<Response>((resolve) => {
            resolveStaleRequest = resolve;
          }),
      )
      .mockResolvedValueOnce(
        new Response(JSON.stringify({ id: "u1", email: "u@example.com" }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      );
    vi.stubGlobal("fetch", fetchMock);

    const client = new ApiClient("https://api.example.test", {
      onUnauthorized,
    });
    client.setToken("old-token");

    const staleRequest = client.getMe().catch((err: unknown) => err);
    expect(fetchMock.mock.calls[0]?.[1]?.headers).toMatchObject({
      Authorization: "Bearer old-token",
    });

    client.setToken("new-token");
    resolveStaleRequest?.(
      new Response(JSON.stringify({ error: "invalid token" }), {
        status: 401,
        statusText: "Unauthorized",
        headers: { "Content-Type": "application/json" },
      }),
    );

    await expect(staleRequest).resolves.toBeInstanceOf(ApiError);
    expect(onUnauthorized).not.toHaveBeenCalled();

    await client.getMe();
    expect(fetchMock.mock.calls[1]?.[1]?.headers).toMatchObject({
      Authorization: "Bearer new-token",
    });
  });

  it("preserves HTTP status on failed requests", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ error: "workspace slug already exists" }), {
          status: 409,
          statusText: "Conflict",
          headers: { "Content-Type": "application/json" },
        }),
      ),
    );

    const client = new ApiClient("https://api.example.test");

    try {
      await client.createWorkspace({ name: "Test", slug: "test" });
      throw new Error("expected createWorkspace to fail");
    } catch (error) {
      expect(error).toBeInstanceOf(ApiError);
      expect(error).toMatchObject({
        message: "workspace slug already exists",
        status: 409,
        statusText: "Conflict",
      });
    }
  });

  it("uses the state-based Google binding callback endpoint", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ binding: {}, next_path: "/settings" }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const client = new ApiClient("https://api.example.test");
    await client.completeGoogleBinding("code-1", "google.signed-state");

    expect(fetchMock).toHaveBeenCalledWith(
      "https://api.example.test/api/notification-bindings/google/callback",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({ code: "code-1", state: "google.signed-state" }),
      }),
    );
  });

  it("uses the expected HTTP contract for autopilot endpoints", async () => {
    const fetchMock = vi.fn().mockImplementation(() => Promise.resolve(
      new Response(JSON.stringify({ autopilots: [], runs: [], total: 0 }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    ));
    vi.stubGlobal("fetch", fetchMock);

    const client = new ApiClient("https://api.example.test");

    await client.listAutopilots({ status: "active" });
    await client.getAutopilot("ap-1");
    await client.createAutopilot({
      title: "Daily triage",
      project_id: "project-1",
      assignee_id: "agent-1",
      execution_mode: "create_issue",
    });
    await client.updateAutopilot("ap-1", { status: "paused", project_id: null });
    await client.deleteAutopilot("ap-1");
    await client.triggerAutopilot("ap-1");
    await client.triggerAutopilot("ap-1", { trigger_payload: "production" });
    await client.listAutopilotRuns("ap-1", { limit: 10, offset: 20 });
    await client.createAutopilotTrigger("ap-1", {
      kind: "schedule",
      cron_expression: "0 9 * * *",
      timezone: "UTC",
    });
    await client.updateAutopilotTrigger("ap-1", "tr-1", { enabled: false });
    await client.deleteAutopilotTrigger("ap-1", "tr-1");
    await client.rotateAutopilotTriggerWebhookToken("ap-1", "tr-1");

    const calls = fetchMock.mock.calls.map(([url, init]) => ({
      url,
      method: init?.method ?? "GET",
      body: init?.body,
    }));

    expect(calls).toMatchObject([
      { url: "https://api.example.test/api/autopilots?status=active", method: "GET" },
      { url: "https://api.example.test/api/autopilots/ap-1", method: "GET" },
      {
        url: "https://api.example.test/api/autopilots",
        method: "POST",
        body: JSON.stringify({
          title: "Daily triage",
          project_id: "project-1",
          assignee_id: "agent-1",
          execution_mode: "create_issue",
        }),
      },
      {
        url: "https://api.example.test/api/autopilots/ap-1",
        method: "PATCH",
        body: JSON.stringify({ status: "paused", project_id: null }),
      },
      { url: "https://api.example.test/api/autopilots/ap-1", method: "DELETE" },
      { url: "https://api.example.test/api/autopilots/ap-1/trigger", method: "POST" },
      {
        url: "https://api.example.test/api/autopilots/ap-1/trigger",
        method: "POST",
        body: JSON.stringify({ trigger_payload: "production" }),
      },
      { url: "https://api.example.test/api/autopilots/ap-1/runs?limit=10&offset=20", method: "GET" },
      {
        url: "https://api.example.test/api/autopilots/ap-1/triggers",
        method: "POST",
        body: JSON.stringify({
          kind: "schedule",
          cron_expression: "0 9 * * *",
          timezone: "UTC",
        }),
      },
      {
        url: "https://api.example.test/api/autopilots/ap-1/triggers/tr-1",
        method: "PATCH",
        body: JSON.stringify({ enabled: false }),
      },
      { url: "https://api.example.test/api/autopilots/ap-1/triggers/tr-1", method: "DELETE" },
      {
        url: "https://api.example.test/api/autopilots/ap-1/triggers/tr-1/rotate-webhook-token",
        method: "POST",
      },
    ]);
  });

  it("emits X-Client-* headers when identity is configured", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify([]), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const client = new ApiClient("https://api.example.test", {
      identity: { platform: "desktop", version: "1.2.3", os: "macos" },
    });
    await client.listWorkspaces();

    const headers = fetchMock.mock.calls[0]![1]!.headers as Record<string, string>;
    expect(headers["X-Client-Platform"]).toBe("desktop");
    expect(headers["X-Client-Version"]).toBe("1.2.3");
    expect(headers["X-Client-OS"]).toBe("macos");
  });

  it("omits X-Client-* headers when identity is not configured", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify([]), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const client = new ApiClient("https://api.example.test");
    await client.listWorkspaces();

    const headers = fetchMock.mock.calls[0]![1]!.headers as Record<string, string>;
    expect(headers["X-Client-Platform"]).toBeUndefined();
    expect(headers["X-Client-Version"]).toBeUndefined();
    expect(headers["X-Client-OS"]).toBeUndefined();
  });

  it("uses the Cloud Runtime node API contract", async () => {
    const node = {
      id: "node-1",
      owner_id: "user-1",
      instance_id: "i-0123456789abcdef0",
      region: "us-west-2",
      instance_type: "g5.xlarge",
      image_id: "ami-1",
      subnet_id: "subnet-1",
      name: "gpu-dev-01",
      status: "launching",
      tags: {},
      metadata: {},
      created_at: "2026-05-21T08:30:00Z",
      updated_at: "2026-05-21T08:30:00Z",
    };
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        new Response(JSON.stringify([]), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      )
      .mockResolvedValueOnce(
        new Response(JSON.stringify(node), {
          status: 201,
          headers: { "Content-Type": "application/json" },
        }),
      );
    vi.stubGlobal("fetch", fetchMock);

    const client = new ApiClient("https://api.example.test");
    await client.listCloudRuntimeNodes({ limit: 20, offset: 5 });
    await client.createCloudRuntimeNode(
      { instance_type: "g5.xlarge", name: "gpu-dev-01" },
    );

    const listCall = fetchMock.mock.calls[0]!;
    const createCall = fetchMock.mock.calls[1]!;
    expect(listCall[0]).toBe(
      "https://api.example.test/api/cloud-runtime/nodes?limit=20&offset=5",
    );
    expect(createCall[0]).toBe(
      "https://api.example.test/api/cloud-runtime/nodes",
    );
    expect(createCall[1]).toMatchObject({
      method: "POST",
      body: JSON.stringify({
        instance_type: "g5.xlarge",
        name: "gpu-dev-01",
      }),
    });
  });

  it("falls back when Cloud Runtime node responses drift", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        new Response(JSON.stringify([{ id: 123 }]), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      )
      .mockResolvedValueOnce(
        new Response(JSON.stringify({ id: 123 }), {
          status: 201,
          headers: { "Content-Type": "application/json" },
        }),
      );
    vi.stubGlobal("fetch", fetchMock);

    const client = new ApiClient("https://api.example.test");

    await expect(client.listCloudRuntimeNodes()).resolves.toEqual([]);
    await expect(
      client.createCloudRuntimeNode({ instance_type: "g5.xlarge" }),
    ).resolves.toMatchObject({ id: "", status: "" });
  });

  it("deleteCloudRuntimeNode sends DELETE with JSON body containing instance id", async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(
      new Response(null, { status: 204 }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const client = new ApiClient("https://api.example.test");
    await client.deleteCloudRuntimeNode("i-0123456789abcdef0");

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, opts] = fetchMock.mock.calls[0]!;
    expect(url).toBe("https://api.example.test/api/cloud-runtime/nodes");
    expect(opts).toMatchObject({
      method: "DELETE",
      body: JSON.stringify({ instance_id: "i-0123456789abcdef0" }),
    });
    expect((opts.headers as Record<string, string>)["Content-Type"]).toBe(
      "application/json",
    );
  });

  describe("getAttachment", () => {
    it("returns the parsed attachment for a well-formed response", async () => {
      vi.stubGlobal(
        "fetch",
        vi.fn().mockResolvedValue(
          new Response(
            JSON.stringify({
              id: "att-1",
              workspace_id: "ws-1",
              issue_id: null,
              comment_id: null,
              uploader_type: "member",
              uploader_id: "u-1",
              filename: "report.md",
              url: "https://static.example.test/ws/att-1.md",
              download_url:
                "https://static.example.test/ws/att-1.md?Policy=p&Signature=s&Key-Pair-Id=k",
              content_type: "text/markdown",
              size_bytes: 123,
              created_at: "2026-05-11T00:00:00Z",
            }),
            { status: 200, headers: { "Content-Type": "application/json" } },
          ),
        ),
      );

      const client = new ApiClient("https://api.example.test");
      const att = await client.getAttachment("att-1");

      expect(att.id).toBe("att-1");
      expect(att.download_url).toContain("Policy=");
    });

    it("falls back to an empty attachment when the response is missing download_url", async () => {
      vi.stubGlobal(
        "fetch",
        vi.fn().mockResolvedValue(
          new Response(JSON.stringify({ id: "att-1" }), {
            status: 200,
            headers: { "Content-Type": "application/json" },
          }),
        ),
      );

      const client = new ApiClient("https://api.example.test");
      const att = await client.getAttachment("att-1");

      // parseWithFallback returns the EMPTY_ATTACHMENT record so callers can
      // safely read `download_url` without crashing — they'll see "" and
      // surface a user-facing error instead of opening `undefined`.
      expect(att.id).toBe("");
      expect(att.download_url).toBe("");
    });
  });

  describe("getAttachmentTextContent", () => {
    it("returns body text and the original content type from the X-* header", async () => {
      vi.stubGlobal(
        "fetch",
        vi.fn().mockResolvedValue(
          new Response("# heading\n\nbody\n", {
            status: 200,
            headers: {
              "Content-Type": "text/plain; charset=utf-8",
              "X-Original-Content-Type": "text/markdown",
            },
          }),
        ),
      );

      const client = new ApiClient("https://api.example.test");
      const { text, originalContentType } =
        await client.getAttachmentTextContent("att-1");

      expect(text).toBe("# heading\n\nbody\n");
      expect(originalContentType).toBe("text/markdown");
    });

    it("throws PreviewTooLargeError on 413", async () => {
      const { PreviewTooLargeError } = await import("./client");
      vi.stubGlobal(
        "fetch",
        vi.fn().mockResolvedValue(
          new Response("", { status: 413, statusText: "Payload Too Large" }),
        ),
      );

      const client = new ApiClient("https://api.example.test");
      await expect(client.getAttachmentTextContent("att-1")).rejects.toBeInstanceOf(
        PreviewTooLargeError,
      );
    });

    it("throws PreviewUnsupportedError on 415", async () => {
      const { PreviewUnsupportedError } = await import("./client");
      vi.stubGlobal(
        "fetch",
        vi.fn().mockResolvedValue(
          new Response("", { status: 415, statusText: "Unsupported Media Type" }),
        ),
      );

      const client = new ApiClient("https://api.example.test");
      await expect(client.getAttachmentTextContent("att-1")).rejects.toBeInstanceOf(
        PreviewUnsupportedError,
      );
    });
  });

  describe("chat attachment wiring", () => {
    it("uploadFile uses direct upload for chat attachments", async () => {
      const fetchMock = vi.fn()
        .mockResolvedValueOnce(
          new Response(
            JSON.stringify({
              upload_url: "https://obs.example.test/object",
              headers: { "Content-Type": "image/png" },
              upload_token: "token-1",
            }),
            { status: 200, headers: { "Content-Type": "application/json" } },
          ),
        )
        .mockResolvedValueOnce(new Response("", { status: 200 }))
        .mockResolvedValueOnce(
          new Response(
            JSON.stringify({
              id: "att-1",
              workspace_id: "ws-1",
              issue_id: null,
              comment_id: null,
              chat_session_id: "session-123",
              chat_message_id: null,
              uploader_type: "member",
              uploader_id: "u-1",
              filename: "hi.png",
              url: "https://cdn/x",
              download_url: "https://cdn/x",
              content_type: "image/png",
              size_bytes: 2,
              created_at: "2026-05-21T00:00:00Z",
            }),
            { status: 200, headers: { "Content-Type": "application/json" } },
          ),
        );
      vi.stubGlobal("fetch", fetchMock);

      const client = new ApiClient("https://api.example.test");
      const file = new File(["hi"], "hi.png", { type: "image/png" });
      const attachment = await client.uploadFile(file, { chatSessionId: "session-123" });

      expect(fetchMock).toHaveBeenCalledTimes(3);
      expect(fetchMock.mock.calls[0]?.[0]).toBe("https://api.example.test/api/attachments/upload/initiate");
      expect(fetchMock.mock.calls[0]?.[1]?.method).toBe("POST");
      expect(JSON.parse(fetchMock.mock.calls[0]?.[1]?.body as string)).toMatchObject({
        filename: "hi.png",
        content_type: "image/png",
        size_bytes: 2,
        chat_session_id: "session-123",
      });
      expect(fetchMock.mock.calls[1]?.[0]).toBe("https://obs.example.test/object");
      expect(fetchMock.mock.calls[1]?.[1]).toMatchObject({
        method: "PUT",
        body: file,
      });
      expect(fetchMock.mock.calls[2]?.[0]).toBe("https://api.example.test/api/attachments/upload/complete");
      expect(attachment.id).toBe("att-1");
    });

    it("uploadFile uses direct upload even without an attachment context", async () => {
      const fetchMock = vi.fn()
        .mockResolvedValueOnce(
          new Response(
            JSON.stringify({
              upload_url: "https://obs.example.test/object",
              headers: { "Content-Type": "image/png" },
              upload_token: "token-1",
            }),
            { status: 200, headers: { "Content-Type": "application/json" } },
          ),
        )
        .mockResolvedValueOnce(new Response("", { status: 200 }))
        .mockResolvedValueOnce(
          new Response(
            JSON.stringify({
              id: "att-1",
              workspace_id: "ws-1",
              issue_id: null,
              comment_id: null,
              chat_session_id: null,
              chat_message_id: null,
              uploader_type: "member",
              uploader_id: "u-1",
              filename: "hi.png",
              url: "https://cdn/x",
              download_url: "https://cdn/x",
              content_type: "image/png",
              size_bytes: 2,
              created_at: "2026-05-21T00:00:00Z",
            }),
            { status: 200, headers: { "Content-Type": "application/json" } },
          ),
        );
      vi.stubGlobal("fetch", fetchMock);

      const client = new ApiClient("https://api.example.test");
      const file = new File(["hi"], "hi.png", { type: "image/png" });
      const attachment = await client.uploadFile(file);

      expect(fetchMock).toHaveBeenCalledTimes(3);
      expect(fetchMock.mock.calls[0]?.[0]).toBe("https://api.example.test/api/attachments/upload/initiate");
      expect(JSON.parse(fetchMock.mock.calls[0]?.[1]?.body as string)).toMatchObject({
        filename: "hi.png",
        content_type: "image/png",
        size_bytes: 2,
        issue_id: null,
        comment_id: null,
        chat_session_id: null,
      });
      expect(fetchMock.mock.calls[1]?.[0]).toBe("https://obs.example.test/object");
      expect(fetchMock.mock.calls[2]?.[0]).toBe("https://api.example.test/api/attachments/upload/complete");
      expect(fetchMock.mock.calls.some((call) => String(call[0]).endsWith("/api/upload-file"))).toBe(false);
      expect(attachment.id).toBe("att-1");
    });

    it("uploadFile does not fall back to /api/upload-file when direct upload is unsupported", async () => {
      const fetchMock = vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ error: "direct upload not supported" }), {
          status: 501,
          statusText: "Not Implemented",
          headers: { "Content-Type": "application/json" },
        }),
      );
      vi.stubGlobal("fetch", fetchMock);

      const client = new ApiClient("https://api.example.test");
      const file = new File(["hi"], "hi.png", { type: "image/png" });

      await expect(client.uploadFile(file, { chatSessionId: "session-123" })).rejects.toMatchObject({
        status: 501,
        message: "direct upload not supported",
      });
      expect(fetchMock).toHaveBeenCalledTimes(1);
      expect(fetchMock.mock.calls[0]?.[0]).toBe("https://api.example.test/api/attachments/upload/initiate");
      expect(fetchMock.mock.calls.some((call) => String(call[0]).endsWith("/api/upload-file"))).toBe(false);
    });

    it("uploadFile does not fall back to /api/upload-file when multipart direct upload is unsupported", async () => {
      const fetchMock = vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ error: "direct upload not supported" }), {
          status: 501,
          statusText: "Not Implemented",
          headers: { "Content-Type": "application/json" },
        }),
      );
      vi.stubGlobal("fetch", fetchMock);

      const client = new ApiClient("https://api.example.test");
      const file = new File([new Blob([new Uint8Array(64 * 1024 * 1024)])], "large.bin", {
        type: "application/octet-stream",
      });

      await expect(client.uploadFile(file, { chatSessionId: "session-123" })).rejects.toMatchObject({
        status: 501,
        message: "direct upload not supported",
      });
      expect(fetchMock).toHaveBeenCalledTimes(1);
      expect(fetchMock.mock.calls[0]?.[0]).toBe("https://api.example.test/api/attachments/upload/multipart/initiate");
      expect(fetchMock.mock.calls.some((call) => String(call[0]).endsWith("/api/upload-file"))).toBe(false);
    });

    it("uploadFile uses multipart direct upload for large unbound attachments", async () => {
      const fetchMock = vi.fn().mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = String(input);
        if (url.endsWith("/api/attachments/upload/multipart/initiate")) {
          return new Response(
            JSON.stringify({
              session_id: "session-upload-1",
              attachment_id: "att-1",
              object_key: "workspaces/ws/att-1.bin",
              upload_id: "upload-1",
              part_size_bytes: 16 * 1024 * 1024,
              part_count: 4,
              expires_at: "2026-05-21T00:30:00Z",
            }),
            { status: 200, headers: { "Content-Type": "application/json" } },
          );
        }
        if (url.endsWith("/api/attachments/upload/multipart/sign-parts")) {
          const body = JSON.parse(init?.body as string) as { part_numbers: number[] };
          const partNumber = body.part_numbers[0];
          return new Response(
            JSON.stringify({ parts: [{ part_number: partNumber, upload_url: `https://obs.example.test/part-${partNumber}` }] }),
            { status: 200, headers: { "Content-Type": "application/json" } },
          );
        }
        if (url.startsWith("https://obs.example.test/part-")) {
          const partNumber = url.split("-").pop();
          return new Response("", { status: 200, headers: { ETag: `"etag-${partNumber}"` } });
        }
        if (url.endsWith("/api/attachments/upload/multipart/complete")) {
          return new Response(
            JSON.stringify({
              id: "att-1",
              workspace_id: "ws-1",
              issue_id: null,
              comment_id: null,
              chat_session_id: null,
              chat_message_id: null,
              uploader_type: "member",
              uploader_id: "u-1",
              filename: "large.bin",
              url: "https://cdn/x",
              download_url: "https://cdn/x",
              content_type: "application/octet-stream",
              size_bytes: 64 * 1024 * 1024,
              created_at: "2026-05-21T00:00:00Z",
            }),
            { status: 200, headers: { "Content-Type": "application/json" } },
          );
        }
        throw new Error(`unexpected fetch ${url}`);
      });
      vi.stubGlobal("fetch", fetchMock);

      const client = new ApiClient("https://api.example.test");
      const file = new File([new Blob([new Uint8Array(64 * 1024 * 1024)])], "large.bin", {
        type: "application/octet-stream",
      });
      const attachment = await client.uploadFile(file);

      expect(fetchMock.mock.calls[0]?.[0]).toBe("https://api.example.test/api/attachments/upload/multipart/initiate");
      expect(JSON.parse(fetchMock.mock.calls[0]?.[1]?.body as string)).toMatchObject({
        filename: "large.bin",
        content_type: "application/octet-stream",
        size_bytes: 64 * 1024 * 1024,
        issue_id: null,
        comment_id: null,
        chat_session_id: null,
      });
      expect(fetchMock.mock.calls.some((call) => String(call[0]).endsWith("/api/upload-file"))).toBe(false);
      expect(attachment.id).toBe("att-1");
    });

    it("uploadFile uses multipart direct upload for large chat attachments", async () => {
      const fetchMock = vi.fn().mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = String(input);
        if (url.endsWith("/api/attachments/upload/multipart/initiate")) {
          return new Response(
            JSON.stringify({
              session_id: "session-upload-1",
              attachment_id: "att-1",
              object_key: "workspaces/ws/att-1.bin",
              upload_id: "upload-1",
              part_size_bytes: 16 * 1024 * 1024,
              part_count: 4,
              expires_at: "2026-05-21T00:30:00Z",
            }),
            { status: 200, headers: { "Content-Type": "application/json" } },
          );
        }
        if (url.endsWith("/api/attachments/upload/multipart/sign-parts")) {
          const body = JSON.parse(init?.body as string) as { part_numbers: number[] };
          const partNumber = body.part_numbers[0];
          return new Response(
            JSON.stringify({ parts: [{ part_number: partNumber, upload_url: `https://obs.example.test/part-${partNumber}` }] }),
            { status: 200, headers: { "Content-Type": "application/json" } },
          );
        }
        if (url.startsWith("https://obs.example.test/part-")) {
          const partNumber = url.split("-").pop();
          return new Response("", { status: 200, headers: { ETag: `"etag-${partNumber}"` } });
        }
        if (url.endsWith("/api/attachments/upload/multipart/complete")) {
          return new Response(
            JSON.stringify({
              id: "att-1",
              workspace_id: "ws-1",
              issue_id: null,
              comment_id: null,
              chat_session_id: "session-123",
              chat_message_id: null,
              uploader_type: "member",
              uploader_id: "u-1",
              filename: "large.bin",
              url: "https://cdn/x",
              download_url: "https://cdn/x",
              content_type: "application/octet-stream",
              size_bytes: 64 * 1024 * 1024,
              created_at: "2026-05-21T00:00:00Z",
            }),
            { status: 200, headers: { "Content-Type": "application/json" } },
          );
        }
        throw new Error(`unexpected fetch ${url}`);
      });
      vi.stubGlobal("fetch", fetchMock);

      const client = new ApiClient("https://api.example.test");
      const file = new File([new Blob([new Uint8Array(64 * 1024 * 1024)])], "large.bin", {
        type: "application/octet-stream",
      });
      const attachment = await client.uploadFile(file, { chatSessionId: "session-123" });

      expect(fetchMock.mock.calls[0]?.[0]).toBe("https://api.example.test/api/attachments/upload/multipart/initiate");
      expect(fetchMock.mock.calls.filter((call) => String(call[0]).endsWith("/api/attachments/upload/multipart/sign-parts"))).toHaveLength(4);
      expect(fetchMock.mock.calls.filter((call) => String(call[0]).startsWith("https://obs.example.test/part-"))).toHaveLength(4);
      const completeCall = fetchMock.mock.calls.find((call) => String(call[0]).endsWith("/api/attachments/upload/multipart/complete"));
      expect(completeCall).toBeTruthy();
      const completeBody = JSON.parse(completeCall?.[1]?.body as string);
      expect(completeBody.parts).toHaveLength(4);
      expect(attachment.id).toBe("att-1");
    });

    it("uploadFile falls back to an empty attachment when complete returns a malformed response", async () => {
      const fetchMock = vi.fn()
        .mockResolvedValueOnce(
          new Response(
            JSON.stringify({
              upload_url: "https://obs.example.test/object",
              headers: { "Content-Type": "image/png" },
              upload_token: "token-1",
            }),
            { status: 200, headers: { "Content-Type": "application/json" } },
          ),
        )
        .mockResolvedValueOnce(new Response("", { status: 200 }))
        .mockResolvedValueOnce(
          new Response(JSON.stringify({ id: "", url: "https://cdn/x", filename: "hi.png" }), {
            status: 200,
            headers: { "Content-Type": "application/json" },
          }),
        );
      vi.stubGlobal("fetch", fetchMock);

      const client = new ApiClient("https://api.example.test");
      const file = new File(["hi"], "hi.png", { type: "image/png" });
      const attachment = await client.uploadFile(file);

      expect(fetchMock).toHaveBeenCalledTimes(3);
      expect(fetchMock.mock.calls[0]?.[0]).toBe("https://api.example.test/api/attachments/upload/initiate");
      expect(fetchMock.mock.calls[1]?.[0]).toBe("https://obs.example.test/object");
      expect(fetchMock.mock.calls[2]?.[0]).toBe("https://api.example.test/api/attachments/upload/complete");
      expect(attachment.id).toBe("");
      expect(attachment.download_url).toBe("");
    });

    it("sendChatMessage serialises attachment_ids onto the JSON body when present", async () => {
      const fetchMock = vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ message_id: "m1", task_id: "t1", created_at: "" }), {
          status: 201,
          headers: { "Content-Type": "application/json" },
        }),
      );
      vi.stubGlobal("fetch", fetchMock);

      const client = new ApiClient("https://api.example.test");
      await client.sendChatMessage("session-1", "hello", ["att-1", "att-2"]);

      const [, init] = fetchMock.mock.calls[0]!;
      expect(JSON.parse(init?.body as string)).toEqual({
        content: "hello",
        attachment_ids: ["att-1", "att-2"],
      });
    });

    it("sendChatMessage omits attachment_ids when the list is empty or undefined", async () => {
      const fetchMock = vi.fn().mockImplementation(() =>
        Promise.resolve(
          new Response(JSON.stringify({ message_id: "m1", task_id: "t1", created_at: "" }), {
            status: 201,
            headers: { "Content-Type": "application/json" },
          }),
        ),
      );
      vi.stubGlobal("fetch", fetchMock);

      const client = new ApiClient("https://api.example.test");
      await client.sendChatMessage("session-1", "hello");
      await client.sendChatMessage("session-1", "again", []);

      expect(JSON.parse(fetchMock.mock.calls[0]![1]?.body as string)).toEqual({ content: "hello" });
      expect(JSON.parse(fetchMock.mock.calls[1]![1]?.body as string)).toEqual({ content: "again" });
    });
  });
});
