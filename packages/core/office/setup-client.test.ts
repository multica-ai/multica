import { afterEach, describe, expect, it, vi } from "vitest";
import { AGENT_INSTRUCTION_TEMPLATES } from "../agents/instruction-templates";
import { ApiClient, ApiError } from "../api/client";
import { buildOfficeSetupRequest } from "./setup-query";
import {
  FALLBACK_OFFICE_SETUP_READINESS,
  parseOfficeSetupRefusal,
  type OfficeSetupReadiness,
} from "./setup-schema";

afterEach(() => {
  vi.unstubAllGlobals();
});

function respond(body: unknown, status = 200) {
  const fetchMock = vi.fn().mockResolvedValue(
    new Response(body === undefined ? "" : JSON.stringify(body), {
      status,
      headers: { "Content-Type": "application/json" },
    }),
  );
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

const complete: OfficeSetupReadiness = {
  runtime_online: true,
  secretary_id: "agent-1",
  executive_floor: true,
  autopilot_daily: true,
  autopilot_weekly_paused: true,
  ready: true,
};

describe("ApiClient Secretary setup", () => {
  it("GET /api/office/setup reads the checklist", async () => {
    const fetchMock = respond(complete);

    const out = await new ApiClient("https://api.test").getOfficeSetup();

    expect(out).toEqual(complete);
    expect(fetchMock.mock.calls[0]?.[0]).toBe("https://api.test/api/office/setup");
  });

  it("GET falls back to nothing set up for a malformed checklist", async () => {
    for (const body of [null, [], { runtime_online: "yes" }, { secretary_id: 5 }]) {
      respond(body);
      expect(await new ApiClient("https://api.test").getOfficeSetup(), JSON.stringify(body)).toEqual(
        FALLBACK_OFFICE_SETUP_READINESS,
      );
    }
  });

  it("POST /api/office/setup sends the template text and timezone, then reads the checklist", async () => {
    const fetchMock = respond(complete);
    const request = { secretary_instructions: "You are the Secretary.", timezone: "Europe/Paris" };

    const out = await new ApiClient("https://api.test").setupOffice(request);

    expect(out).toEqual(complete);
    expect(fetchMock.mock.calls[0]?.[0]).toBe("https://api.test/api/office/setup");
    expect(fetchMock.mock.calls[0]?.[1]?.method).toBe("POST");
    expect(JSON.parse(String(fetchMock.mock.calls[0]?.[1]?.body))).toEqual(request);
  });

  it("POST falls back to nothing set up for a malformed response", async () => {
    respond({ runtime_online: true, secretary_id: { id: "a" } });

    const out = await new ApiClient("https://api.test").setupOffice({ secretary_instructions: "x" });

    expect(out).toEqual(FALLBACK_OFFICE_SETUP_READINESS);
  });

  it("POST rejects with the runtime_required refusal the card can read", async () => {
    respond(
      {
        error: "an online Hermes runtime is required before the Secretary can be set up",
        code: "runtime_required",
        hint: "Install Hermes on the machine that runs the Multica daemon.",
        docs_path: "/install-agent-runtime",
      },
      409,
    );

    const err = await new ApiClient("https://api.test")
      .setupOffice({ secretary_instructions: "x" })
      .catch((e: unknown) => e);

    expect(err).toBeInstanceOf(ApiError);
    expect(parseOfficeSetupRefusal((err as ApiError).body)).toMatchObject({
      code: "runtime_required",
      docs_path: "/install-agent-runtime",
    });
  });
});

describe("buildOfficeSetupRequest", () => {
  const secretary = AGENT_INSTRUCTION_TEMPLATES.find((t) => t.id === "secretary")!;

  it("sends the secretary template text, so every caller creates the same Secretary", () => {
    expect(buildOfficeSetupRequest("Europe/Paris").secretary_instructions).toBe(secretary.instructions);
  });

  it("uses the timezone it is given", () => {
    expect(buildOfficeSetupRequest("Asia/Tokyo")).toMatchObject({ timezone: "Asia/Tokyo" });
  });

  it("uses the browser's timezone when none is given", () => {
    expect(buildOfficeSetupRequest()).toMatchObject({
      timezone: Intl.DateTimeFormat().resolvedOptions().timeZone,
    });
  });
});
