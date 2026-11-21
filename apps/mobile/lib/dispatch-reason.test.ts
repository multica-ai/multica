// @vitest-environment node
import { describe, expect, it } from "vitest";
import { dispatchReasonCode, sendFailureMessage } from "./dispatch-reason";
import { i18n } from "./i18n/singleton";
import type { SupportedLocale } from "./i18n/types";

// Shaped like `apps/mobile/data/api.ts:ApiError` — a thrown Error carrying the
// parsed response body. Built inline rather than imported because `@/data/api`
// pulls react-native into a node-environment suite; the only field under test
// is `body`, which is what the helper reads.
const apiError = (body: unknown) => Object.assign(new Error("request failed"), { body });

describe("dispatchReasonCode", () => {
  it("reads reason_code off a structured rejection body", () => {
    const err = apiError({
      error: "invocation not allowed",
      reason_code: "invocation_not_allowed",
    });
    expect(dispatchReasonCode(err)).toBe("invocation_not_allowed");
  });

  it("returns undefined for an unstructured failure", () => {
    expect(dispatchReasonCode(new Error("network down"))).toBeUndefined();
    expect(dispatchReasonCode(apiError(undefined))).toBeUndefined();
    expect(dispatchReasonCode(apiError("plain text body"))).toBeUndefined();
    expect(dispatchReasonCode(null)).toBeUndefined();
  });

  it("ignores a non-string or empty reason_code", () => {
    expect(dispatchReasonCode(apiError({ reason_code: 7 }))).toBeUndefined();
    expect(dispatchReasonCode(apiError({ reason_code: "" }))).toBeUndefined();
  });
});

describe("sendFailureMessage", () => {
  const send = (locale: SupportedLocale, err: unknown) =>
    sendFailureMessage(err, i18n.getFixedT(locale, "chat"));

  // The point of the helper: a revoked permission must not read as a transient
  // failure the user should retry (MUL-6380).
  it("names revoked permission instead of suggesting a retry", () => {
    const err = apiError({ reason_code: "invocation_not_allowed" });
    expect(send("en", err)).toBe(
      "You no longer have permission to run this agent, so the message was not sent.",
    );
    expect(send("zh-Hans", err)).toBe("你已无权运行该智能体，消息未发送。");
    expect(send("zh-Hant", err)).toBe("你已無權執行該 AI 代理，訊息未傳送。");
  });

  it("names the runtime for agent_runtime_required", () => {
    const err = apiError({ reason_code: "agent_runtime_required" });
    expect(send("en", err)).toBe(
      "Bind a runtime to this agent before sending a message.",
    );
    expect(send("zh-Hans", err)).toBe("发送消息前，请先为该智能体绑定运行时。");
    expect(send("zh-Hant", err)).toBe("傳送訊息前，請先為該 AI 代理綁定執行時。");
  });

  // PUCK-89: a private-runtime owner mismatch never resolves by retrying. The
  // message must name the real fixes instead of the generic retry advice.
  it("gives runtime_access_denied actionable copy, not a retry", () => {
    const err = apiError({ reason_code: "runtime_access_denied" });
    expect(send("en", err)).toBe(
      "Message not sent — This agent's private runtime isn't accessible. Make the runtime public, or bind the agent to an accessible runtime.",
    );
    expect(send("zh-Hans", err)).toBe(
      "消息未发送：无法访问该智能体的私有运行时。请将该运行时设为公开，或将智能体绑定到可用的运行时。",
    );
    expect(send("zh-Hant", err)).toBe(
      "訊息未傳送：無法存取該 AI 代理的私有執行時。請將該執行時設為公開，或將 AI 代理綁定到可用的執行時。",
    );
  });

  it("falls back to a retryable message for anything else", () => {
    const err = new Error("timeout");
    expect(send("en", err)).toBe("Your message could not be sent. Please try again.");
    expect(send("zh-Hans", err)).toBe("消息无法发送，请重试。");
    expect(send("zh-Hant", err)).toBe("訊息無法傳送，請重試。");
  });
});
