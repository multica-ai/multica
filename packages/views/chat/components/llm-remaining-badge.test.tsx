import { act, fireEvent, screen, waitFor } from "@testing-library/react";
import { renderWithI18n } from "../../test/i18n";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { llmLimitStatusSchema } from "@multica/core/api/schemas";
import { retainKnownQuota, LlmRemainingBadge } from "./llm-remaining-badge";

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
      "Claude 5시간 잔량 75%; Claude 1주 잔량 40%",
    );
    expect(document.querySelector("[data-acceptance='chat-gpt-token-remaining-badge']")).toHaveAttribute(
      "aria-label",
      "GPT 5시간 잔량 90%; GPT 1주 잔량 70%",
    );
    expect(
      screen.getByLabelText(
        "채팅 LLM 잔량: Claude 5시간 75%, Claude 1주 40%, GPT 5시간 90%, GPT 1주 70%",
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
      "GPT 5시간 잔량 확인 불가; GPT 1주 잔량 확인 불가",
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
  it("keeps the weekly D-day even when the five-hour window is more used", async () => {
    const now = Date.parse("2026-11-23T12:40:00Z");
    vi.stubGlobal("fetch", vi.fn(async () => ({ ok: true, json: async () => ({
      five_hour_pct: 75, five_hour_resets_at: "2026-11-23T14:49:59Z",
      seven_day_pct: 52, seven_day_resets_at: "2026-11-26T14:59:59Z",
      gpt_five_hour_pct: 0, gpt_seven_day_pct: 65,
      gpt_seven_resets_at: "2026-11-25T07:17:00Z",
    }) })));
    vi.spyOn(Date, "now").mockReturnValue(now);
    renderBadge();
    const countdown = await screen.findByTestId("chat-reset-countdown");
    expect(countdown.textContent).toBe("C 리셋까지 3일 2시간 19분 · G 리셋까지 1일 18시간 37분");
  });

  it("shows one limiting reset per provider and falls back when its timestamp is missing", async () => {
    const now = Date.parse("2026-11-04T00:00:00Z");
    vi.stubGlobal("fetch", vi.fn(async () => ({ ok: true, json: async () => ({
      five_hour_pct: 25, five_hour_resets_at: "2026-11-04T02:00:00Z",
      seven_day_pct: 85, seven_day_resets_at: "2026-11-05T01:00:00Z",
      gpt_five_hour_pct: 95, gpt_seven_day_pct: 25,
      gpt_seven_resets_at: "2026-11-04T03:00:00Z",
    }) })));
    vi.spyOn(Date, "now").mockReturnValue(now);
    vi.useFakeTimers({ toFake: ["setInterval", "clearInterval"] });
    const view = renderBadge();
    await screen.findByTestId("chat-reset-countdown");
    vi.mocked(Date.now).mockReturnValue(now + 1000);
    act(() => { vi.advanceTimersByTime(1000); });
    const countdown = screen.getByTestId("chat-reset-countdown");
    expect(screen.getAllByTestId("chat-reset-countdown")).toHaveLength(1);
    expect(countdown).toHaveClass("whitespace-nowrap");
    expect(countdown.children).toHaveLength(2);
    expect(countdown.textContent).toBe("C 리셋까지 1일 0시간 59분 · G 리셋까지 2시간 59분");
    expect(countdown.querySelectorAll("time, [title]")).toHaveLength(0);
    expect(countdown).not.toHaveTextContent(/KST|오전|오후|\d월|\d{1,2}:\d{2}/);
    expect(globalThis.fetch).toHaveBeenCalledTimes(1);
    view.unmount();
    vi.useRealTimers();
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it("renders Claude's weekly countdown when the live five-hour window has no reset", async () => {
    const now = Date.now();
    const futureReset = (hours: number) => new Date(now + hours * 3_600_000).toISOString();
    vi.stubGlobal("fetch", vi.fn(async () => ({
      ok: true,
      json: async () => ({
        claude_status: "available",
        five_hour_pct: 0,
        five_hour_resets_at: null,
        seven_day_pct: 38,
        seven_day_resets_at: futureReset(80),
        sonnet_pct: null,
        sonnet_resets_at: null,
        gpt_status: "available",
        gpt_five_hour_pct: 0,
        gpt_five_resets_at: null,
        gpt_seven_day_pct: 56,
        gpt_seven_resets_at: futureReset(55),
      }),
    })));

    try {
      renderBadge();

      const countdown = await screen.findByTestId("chat-reset-countdown");
      expect(countdown.children).toHaveLength(2);
      expect(countdown.children[0]).toHaveTextContent(/^C 리셋까지 3일 \d+시간 \d+분$/);
      expect(countdown.children[1]).toHaveTextContent(/^· G 리셋까지 2일 \d+시간 \d+분$/);
    } finally {
      vi.unstubAllGlobals();
    }
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
    expect(screen.getByTestId("chat-reset-countdown")).toHaveTextContent("C 리셋까지 남은 시간 확인 불가");
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


it("retains only a future Claude reset received with the last observation", () => {
  const future = new Date(Date.now() + 86400000).toISOString();
  const past = new Date(Date.now() - 1000).toISOString();
  const previous = llmLimitStatusSchema.parse({five_hour_pct: 19, seven_day_pct: 83, seven_day_resets_at: future});
  const unavailable = llmLimitStatusSchema.parse({claude_status: "unavailable"});
  expect(retainKnownQuota(previous, unavailable).seven_day_resets_at).toBe(future);
  expect(retainKnownQuota({...previous,seven_day_resets_at:past}, unavailable).seven_day_resets_at).toBeNull();
  expect(retainKnownQuota(undefined,unavailable).seven_day_resets_at).toBeNull();
  expect(llmLimitStatusSchema.parse({claude_status:"stale",seven_day_resets_at:future}).seven_day_resets_at).toBe(future);
});
