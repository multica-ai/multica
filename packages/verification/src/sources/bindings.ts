import { readFile, lstat } from "node:fs/promises";
import { z } from "zod";
import { bindingSchema, type SourceBinding } from "../contracts/resources.js";
import { problem, VerificationError } from "../host/problem.js";

export async function readPrivateFile(path: string): Promise<string> {
  const info = await lstat(path);
  if (
    !info.isFile() ||
    info.isSymbolicLink() ||
    info.size > 1024 * 1024 ||
    (process.platform !== "win32" && (info.mode & 0o077) !== 0)
  )
    throw new VerificationError(
      problem(
        "forbidden",
        "The host binding or credential must be a private regular file",
        "userAction",
      ),
    );
  return readFile(path, "utf8");
}
export async function readBindings(path: string): Promise<SourceBinding[]> {
  return z
    .object({ schemaVersion: z.literal(1), sources: z.array(bindingSchema) })
    .strict()
    .parse(JSON.parse(await readPrivateFile(path))).sources;
}

export class Bindings {
  private readonly values = new Map<string, SourceBinding>();
  constructor(bindings: SourceBinding[] = []) {
    for (const binding of bindings) this.add(binding);
  }
  add(value: SourceBinding): void {
    const binding = bindingSchema.parse(value);
    const url = new URL(binding.baseUrl);
    if (
      url.username ||
      url.password ||
      url.search ||
      url.hash ||
      !["http:", "https:"].includes(url.protocol)
    )
      throw new VerificationError(
        problem("invalidInput", "Invalid source address"),
      );
    if (
      binding.kind !== "backend" &&
      !["127.0.0.1", "localhost", "[::1]"].includes(url.hostname)
    )
      throw new VerificationError(
        problem(
          "forbidden",
          "Desktop and daemon sources must be loopback addresses",
        ),
      );
    if (this.values.has(binding.ref))
      throw new VerificationError(
        problem("requestConflict", "Duplicate host binding reference"),
      );
    this.values.set(binding.ref, binding);
  }
  replace(value: SourceBinding): void {
    const prior = this.values.get(value.ref);
    if (prior && JSON.stringify(prior.scope) !== JSON.stringify(value.scope))
      throw new VerificationError(
        problem(
          "identityMismatch",
          "The environment account or workspace changed; prepare another environment",
        ),
      );
    this.values.delete(value.ref);
    try {
      this.add(value);
    } catch (error) {
      if (prior) this.values.set(prior.ref, prior);
      throw error;
    }
  }
  get(ref: string): SourceBinding {
    const value = this.values.get(ref);
    if (!value)
      throw new VerificationError(
        problem(
          "forbidden",
          "This source has not been bound by the host",
          "userAction",
        ),
      );
    return value;
  }
  async headers(ref: string): Promise<Record<string, string>> {
    const binding = this.get(ref);
    const token = (await readPrivateFile(binding.tokenFile)).trim();
    if (!token || /[\r\n]/.test(token))
      throw new VerificationError(
        problem("forbidden", "Invalid host credential"),
      );
    return {
      Authorization: `Bearer ${token}`,
      "X-Multica-API-Origin": binding.scope.apiOrigin,
      "X-Multica-User-ID": binding.scope.userId,
      "X-Workspace-ID": binding.scope.workspaceId,
      "Content-Type": "application/json",
    };
  }
  async request(
    ref: string,
    path: string,
    method = "GET",
    body?: unknown,
  ): Promise<unknown> {
    const bytes = await this.bytes(ref, path, method, body);
    return bytes.length ? JSON.parse(bytes.toString("utf8")) : null;
  }
  async bytes(
    ref: string,
    path: string,
    method = "GET",
    body?: unknown,
  ): Promise<Buffer> {
    const binding = this.get(ref);
    if (!path.startsWith("/") || path.startsWith("//"))
      throw new VerificationError(
        problem("invalidInput", "Invalid source route"),
      );
    if (method !== "GET" && binding.access !== "test")
      throw new VerificationError(
        problem("forbidden", "This source is read-only"),
      );
    let response: Response;
    try {
      response = await fetch(new URL(path, binding.baseUrl), {
        method,
        headers: await this.headers(ref),
        redirect: "error",
        signal: AbortSignal.timeout(10000),
        ...(body === undefined ? {} : { body: JSON.stringify(body) }),
      });
    } catch {
      throw new VerificationError(
        problem(
          method === "GET" ? "sourceUnavailable" : "outcomeUnknown",
          "The source response was not confirmed; inspect retained resources before another mutation",
          "inspect",
        ),
      );
    }
    if (!response.ok)
      throw new VerificationError(
        problem(
          method !== "GET" && response.status >= 500
            ? "outcomeUnknown"
            : response.status === 401 || response.status === 403
              ? "forbidden"
              : response.status === 409
                ? "requestConflict"
                : response.status === 404
                  ? "unsupportedCapability"
                  : "sourceUnavailable",
          `Source request failed with HTTP ${response.status}`,
          "inspect",
        ),
      );
    const chunks: Uint8Array[] = [];
    let size = 0;
    if (response.status === 204) return Buffer.alloc(0);
    if (!response.body)
      throw new VerificationError(
        problem("sourceUnavailable", "Empty source response"),
      );
    for await (const chunk of response.body) {
      size += chunk.length;
      if (size > 64 * 1024 * 1024) {
        throw new VerificationError(
          problem(
            "coverageIncomplete",
            "Source response exceeds the collection limit",
            "inspect",
          ),
        );
      }
      chunks.push(chunk);
    }
    return Buffer.concat(chunks);
  }
  assertTest(ref: string): SourceBinding {
    const binding = this.get(ref);
    if (binding.access !== "test")
      throw new VerificationError(
        problem("forbidden", "Mutations require a host-bound test source"),
      );
    if (
      !["127.0.0.1", "localhost", "[::1]"].includes(
        new URL(binding.baseUrl).hostname,
      )
    )
      throw new VerificationError(
        problem(
          "forbidden",
          "Test mutations are restricted to the local test environment",
        ),
      );
    return binding;
  }
}
