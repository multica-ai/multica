import { describe, expect, it } from "vitest";
import { applyAuthRequestHeaders } from "./auth-request-headers";

const API_BASE = "https://api.multica.ai";
const TOKEN = "eyJhbGciOiJIUzI1NiJ9.test.token";

describe("applyAuthRequestHeaders — Authorization injection", () => {
  it("injects Authorization header for API requests when token is set", () => {
    const result = applyAuthRequestHeaders(
      {},
      `${API_BASE}/v1/files/123`,
      TOKEN,
      API_BASE,
    );
    expect(result["Authorization"]).toBe(`Bearer ${TOKEN}`);
  });

  it("does not override an existing Authorization header", () => {
    const existing = "Bearer existing-token";
    const result = applyAuthRequestHeaders(
      { Authorization: existing },
      `${API_BASE}/v1/files/123`,
      TOKEN,
      API_BASE,
    );
    expect(result["Authorization"]).toBe(existing);
  });

  it("does not inject Authorization when no token is set", () => {
    const result = applyAuthRequestHeaders(
      {},
      `${API_BASE}/v1/files/123`,
      null,
      API_BASE,
    );
    expect(result["Authorization"]).toBeUndefined();
  });

  it("does not inject Authorization when apiBaseUrl is null", () => {
    const result = applyAuthRequestHeaders(
      {},
      `${API_BASE}/v1/files/123`,
      TOKEN,
      null,
    );
    expect(result["Authorization"]).toBeUndefined();
  });

  it("does not inject Authorization for requests outside the API base", () => {
    const result = applyAuthRequestHeaders(
      {},
      "https://cdn.example.com/image.png",
      TOKEN,
      API_BASE,
    );
    expect(result["Authorization"]).toBeUndefined();
  });

  it("injects Authorization for requests to API sub-paths", () => {
    const result = applyAuthRequestHeaders(
      {},
      `${API_BASE}/static/attachments/abc/preview`,
      TOKEN,
      API_BASE,
    );
    expect(result["Authorization"]).toBe(`Bearer ${TOKEN}`);
  });

  it("preserves all other headers when injecting", () => {
    const result = applyAuthRequestHeaders(
      { "Content-Type": "application/json", "X-Custom": "value" },
      `${API_BASE}/v1/issues`,
      TOKEN,
      API_BASE,
    );
    expect(result["Content-Type"]).toBe("application/json");
    expect(result["X-Custom"]).toBe("value");
    expect(result["Authorization"]).toBe(`Bearer ${TOKEN}`);
  });

  it("does not mutate the original headers object", () => {
    const headers: Record<string, string> = {};
    applyAuthRequestHeaders(headers, `${API_BASE}/v1/files/1`, TOKEN, API_BASE);
    expect(headers["Authorization"]).toBeUndefined();
  });
});

describe("applyAuthRequestHeaders — WebSocket Origin stripping", () => {
  it("removes Origin header from wss:// requests", () => {
    const result = applyAuthRequestHeaders(
      { Origin: "https://localhost:5173" },
      "wss://api.multica.ai/ws",
      null,
      null,
    );
    expect(result["Origin"]).toBeUndefined();
  });

  it("removes Origin header from ws:// requests", () => {
    const result = applyAuthRequestHeaders(
      { Origin: "http://localhost:5173" },
      "ws://localhost:8080/ws",
      null,
      null,
    );
    expect(result["Origin"]).toBeUndefined();
  });

  it("does not touch Origin header for https:// requests", () => {
    const result = applyAuthRequestHeaders(
      { Origin: "https://multica.ai" },
      "https://api.multica.ai/v1/issues",
      null,
      null,
    );
    expect(result["Origin"]).toBe("https://multica.ai");
  });

  it("strips Origin AND injects Authorization for wss:// requests to the API", () => {
    const wsApiBase = "wss://api.multica.ai";
    const result = applyAuthRequestHeaders(
      { Origin: "https://localhost:5173" },
      `${wsApiBase}/ws/events`,
      TOKEN,
      wsApiBase,
    );
    expect(result["Origin"]).toBeUndefined();
    expect(result["Authorization"]).toBe(`Bearer ${TOKEN}`);
  });
});
