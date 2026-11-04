import { act, fireEvent, screen, waitFor } from "@testing-library/react";
import { renderWithI18n } from "../../test/i18n";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { llmLimitStatusSchema } from "@multica/core/api/schemas";
import { formatResetCountdown, retainKnownQuota, LlmRemainingBadge } from "./llm-remaining-badge";

function renderBadge() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return renderWithI18n(
    <QueryClientProvider client={queryClient}>
      <LlmRemainingBadge />
    </QueryClientProvider>,
    { locale: "ko" },
  );
}

describe("LlmRemainingBadge", () => {
  beforeEach(() => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => ({
        ok: true,
        json: async () => ({
          five_hour_pct: 25,
          seven_day_pct: 60,
          sonnet_pct: 27,
          gpt_five_hour_pct: 10,
          gpt_seven_day_pct: 30,
          five_hour_reset_label: "(수) 오후 9:30에 재설정",
          seven_day_reset_label: "(금) 오전 12:00에 재설정",
          sonnet_reset_label: "(토) 오전 9:00에 재설정",
          gpt_five_reset_label: "resets 10:45 PM",
          gpt_seven_reset_label: "resets May 17",
        }),
      })),
    );
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("renders provider-specific 5h and 7d remaining values from the token snapshot API", async () => {
    renderBadge();

    await waitFor(() => {
      expect(screen.getByTestId("chat-llm-gauge-compact")).toHaveTextContent("C: 5h 75%/7d 40% · G: 5h 90%/7d 70%");
    });
    expect(document.querySelector("[data-acceptance='chat-claude-token-remaining-badge']")).toHaveAttribute(
      "aria-label",
      "Claude 5시간 잔량 75%, 리셋 (수) 오후 9:30; Claude 1주 잔량 40%, 리셋 (금) 오전 12:00",
    );
    expect(document.querySelector("[data-acceptance='chat-gpt-token-remaining-badge']")).toHaveAttribute(
      "aria-label",
      "GPT 5시간 잔량 90%, 리셋 10:45 PM; GPT 1주 잔량 70%, 리셋 May 17",
    );
    expect(
      screen.getByLabelText(
        "채팅 LLM 잔량: Claude 5시간 75%, 리셋 (수) 오후 9:30, Claude 1주 40%, 리셋 (금) 오전 12:00, GPT 5시간 90%, 리셋 10:45 PM, GPT 1주 70%, 리셋 May 17",
      ),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByLabelText("채팅 LLM 잔량 새로고침"));
    expect(document.querySelector("[data-acceptance='chat-llm-gauge-manual-refresh']")).toBeTruthy();
    expect(globalThis.fetch).toHaveBeenCalledWith(
      "/api/dashboard/llm-limit-status",
      expect.objectContaining({ cache: "no-store" }),
    );
  });

  it("keeps the live acceptance token gauge as a single compact line", async () => {
    renderBadge();

    const compactGauge = await screen.findByTestId("chat-llm-gauge-compact");

    expect(compactGauge).toHaveTextContent("C: 5h 75%/7d 40% · G: 5h 90%/7d 70%");
    expect(compactGauge).toHaveClass("whitespace-nowrap");
    expect(compactGauge).toHaveAttribute("data-acceptance", "chat-token-gauge");
  });

  it("does not turn unavailable GPT limits into fake 100 percent remaining", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => ({
        ok: true,
        json: async () => ({
          five_hour_pct: 25,
          seven_day_pct: 60,
          sonnet_pct: 27,
          gpt_five_hour_pct: null,
          gpt_seven_day_pct: null,
          five_hour_reset_label: "(수) 오후 9:30에 재설정",
          seven_day_reset_label: "(금) 오전 12:00에 재설정",
          sonnet_reset_label: "(토) 오전 9:00에 재설정",
          gpt_five_reset_label: "—",
          gpt_seven_reset_label: "—",
        }),
      })),
    );

    renderBadge();

    await waitFor(() => {
      expect(screen.getByTestId("chat-llm-gauge-compact")).toHaveTextContent("C: 5h 75%/7d 40% · G: 5h 확인 불가/7d 확인 불가");
    });
    expect(document.querySelector("[data-acceptance='chat-gpt-token-remaining-badge']")).toHaveAttribute(
      "aria-label",
      "GPT 5시간 잔량 확인 불가, 리셋 -; GPT 1주 잔량 확인 불가, 리셋 -",
    );
    expect(screen.queryByText("100%")).not.toBeInTheDocument();
  });
});


it("does not invent quota for malformed API data", async () => {
 vi.stubGlobal("fetch", vi.fn(async () => ({ok: true, json: async () => ({five_hour_pct: "broken"})})));
 renderBadge();
 await waitFor(() => expect(screen.getByTestId("chat-llm-gauge-compact")).toHaveTextContent("C: 5h 확인 불가/7d 확인 불가"));
 vi.unstubAllGlobals();
});


describe("reset countdown", () => {
  it("formats day boundaries, expiry and unknown times without guessing", () => {
    const now = Date.parse("2026-11-04T00:00:00Z");
    expect(formatResetCountdown("2026-11-05T00:00:00Z", now)).toBe("D-1 00:00:00");
    expect(formatResetCountdown("2026-11-04T23:59:59Z", now)).toBe("23:59:59");
    expect(formatResetCountdown("2026-11-03T00:00:00Z", now)).toBe("00:00:00");
    expect(formatResetCountdown(null, now)).toBe("--:--:--");
    expect(formatResetCountdown("invalid", now)).toBe("--:--:--");
  });

  it("updates every second without refetching and gives an exact KST tooltip", async () => {
    const now = Date.parse("2026-11-04T00:00:00Z");
    vi.stubGlobal("fetch", vi.fn(async () => ({ ok: true, json: async () => ({
      five_hour_pct: 25, five_hour_resets_at: "2026-11-04T02:00:00Z",
      seven_day_resets_at: "2026-11-04T01:00:00Z",
    }) })));
    vi.spyOn(Date, "now").mockReturnValue(now);
    vi.useFakeTimers({ toFake: ["setInterval", "clearInterval"] });
    const view = renderBadge();
    await screen.findByTestId("chat-reset-countdown");
    vi.mocked(Date.now).mockReturnValue(now + 1000);
    act(() => { vi.advanceTimersByTime(1000); });
    expect(screen.getByTestId("chat-reset-countdown")).toHaveTextContent("C 리셋 00:59:59");
    expect(screen.getByTestId("chat-reset-countdown")).toHaveTextContent("G 리셋 --:--:--");
    expect(screen.getByTestId("chat-reset-countdown")).not.toHaveTextContent("5h");
    expect(screen.getByTitle(/KST/).title).toContain("10시 0분 0초");
    expect(globalThis.fetch).toHaveBeenCalledTimes(1);
    view.unmount();
    vi.useRealTimers();
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it("drops malformed and unavailable reset timestamps at the API boundary", () => {
    expect(llmLimitStatusSchema.parse({ five_hour_resets_at: 123 }).five_hour_resets_at).toBeNull();
    expect(llmLimitStatusSchema.parse({ five_hour_resets_at: "bad" }).five_hour_resets_at).toBeNull();
    expect(llmLimitStatusSchema.parse({ claude_status: "stale", five_hour_resets_at: "2026-11-04T00:00:00Z" }).five_hour_resets_at).toBeNull();
  });
});


describe("last known quota during failures", () => {
  it("retains observed percentages when a later API response loses a provider", () => {
    const previous = llmLimitStatusSchema.parse({five_hour_pct: 9, seven_day_pct: 82, gpt_five_hour_pct: 0, gpt_seven_day_pct: 1, claude_status: "available", gpt_status: "available", claude_last_observed_at: "2026-11-04T10:38:40Z"});
    const current = llmLimitStatusSchema.parse({claude_status: "unavailable", gpt_status: "unavailable"});
    const result = retainKnownQuota(previous, current);
    expect(result).toMatchObject({five_hour_pct: 9, seven_day_pct: 82, gpt_five_hour_pct: 0, gpt_seven_day_pct: 1, claude_status: "unavailable", gpt_status: "unavailable", claude_last_observed_at: "2026-11-04T10:38:40Z"});
    expect(result.seven_day_resets_at).toBeNull();
    expect(retainKnownQuota(undefined, current).five_hour_pct).toBeNull();
  });

  it("uses the API's last observation on initial load without claiming it is current", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => ({ok: true, json: async () => ({claude_status: "unavailable", claude_last_five_hour_pct: 9, claude_last_seven_day_pct: 82, claude_last_observed_at: "2026-11-04T10:38:40Z"})})));
    renderBadge();
    await waitFor(() => expect(screen.getByTestId("chat-llm-gauge-compact")).toHaveTextContent("C*: 5h 91%/7d 18%"));
    expect(screen.getByTestId("chat-token-gauge").title).toContain("마지막 확인값");
    expect(screen.getByTestId("chat-reset-countdown")).toHaveTextContent("C 리셋 --:--:--");
    vi.unstubAllGlobals();
  });

  it("keeps the last successful percentages when manual refresh fails", async () => {
    const fetcher = vi.fn().mockResolvedValueOnce({ok: true,json: async () => ({five_hour_pct: 9,seven_day_pct: 82,gpt_five_hour_pct: 0,gpt_seven_day_pct: 1})}).mockResolvedValue({ok: false});
    vi.stubGlobal("fetch", fetcher);
    renderBadge();
    await waitFor(() => expect(screen.getByTestId("chat-llm-gauge-compact")).toHaveTextContent("C: 5h 91%/7d 18%"));
    fireEvent.click(screen.getByLabelText("채팅 LLM 잔량 새로고침"));
    await waitFor(() => expect(screen.getByTestId("chat-llm-gauge-compact")).toHaveTextContent("C*: 5h 91%/7d 18% · G*: 5h 100%/7d 99%"));
    vi.unstubAllGlobals();
  });
});


it("marks partial fallback as stale and replaces it with the next complete observation", () => {
  const previous = llmLimitStatusSchema.parse({five_hour_pct: 9, seven_day_pct: 82});
  const partial = retainKnownQuota(previous, llmLimitStatusSchema.parse({five_hour_pct: 10, claude_status: "available"}));
  expect(partial).toMatchObject({five_hour_pct: 10, seven_day_pct: 82, claude_status: "stale"});
  const recovered = retainKnownQuota(partial, llmLimitStatusSchema.parse({five_hour_pct: 11, seven_day_pct: 83, claude_status: "available"}));
  expect(recovered).toMatchObject({five_hour_pct: 11, seven_day_pct: 83, claude_status: "available"});
});

it("marks a retained weekly-only observation", async () => {
  vi.stubGlobal("fetch", vi.fn(async () => ({ok: true,json: async () => ({claude_status: "unavailable",claude_last_seven_day_pct: 82})})));
  renderBadge();
  await waitFor(() => expect(screen.getByTestId("chat-llm-gauge-compact")).toHaveTextContent("C*: 5h 확인 불가/7d 18%"));
  vi.unstubAllGlobals();
});
