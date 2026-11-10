import { afterEach, describe, expect, it, vi } from "vitest";
import type { User } from "../types";
import {
  claimStarPromptShowing,
  markStarPromptStarred,
  STAR_PROMPT_COOLDOWN_MS,
  STAR_PROMPT_MIN_ACCOUNT_AGE_MS,
  starPromptDue,
} from "./index";

const api = vi.hoisted(() => ({
  claimStarPromptShowing: vi.fn(),
  markStarPromptStarred: vi.fn(),
}));
const authStore = vi.hoisted(() => {
  // eslint-disable-next-line @typescript-eslint/no-require-imports
  const { create } = require("zustand") as typeof import("zustand");
  return create<{ user: User | null; setUser: (user: User) => void }>((set) => ({
    user: null,
    setUser: (user) => set({ user }),
  }));
});

vi.mock("../api", () => ({ api }));
vi.mock("../auth", () => ({ useAuthStore: authStore }));

const NOW = Date.parse("2026-11-09T12:00:00Z");
const iso = (ms: number) => new Date(ms).toISOString();

function user(patch: Partial<User> = {}): User {
  return {
    id: "u1",
    name: "Ada",
    email: "ada@example.com",
    avatar_url: null,
    onboarded_at: null,
    onboarding_questionnaire: {},
    created_at: iso(NOW - STAR_PROMPT_MIN_ACCOUNT_AGE_MS - 1),
    updated_at: iso(NOW),
    star_prompt_shown_count: 0,
    star_prompt_last_shown_at: null,
    star_prompt_starred_at: null,
    ...patch,
  } as User;
}

afterEach(() => {
  authStore.setState({ user: null });
  vi.clearAllMocks();
});

describe("starPromptDue", () => {
  it("is due for an established account that has never seen it", () => {
    expect(starPromptDue(user(), NOW)).toBe(true);
  });

  it("is never due when the server cannot count showings", () => {
    expect(starPromptDue(user({ star_prompt_shown_count: undefined }), NOW)).toBe(false);
    expect(starPromptDue(null, NOW)).toBe(false);
  });

  it("waits for the account to be three days old", () => {
    expect(starPromptDue(user({ created_at: iso(NOW - STAR_PROMPT_MIN_ACCOUNT_AGE_MS + 1) }), NOW)).toBe(
      false,
    );
  });

  it("waits out the cooldown after a showing", () => {
    const shown = (ago: number) =>
      user({ star_prompt_shown_count: 1, star_prompt_last_shown_at: iso(NOW - ago) });
    expect(starPromptDue(shown(STAR_PROMPT_COOLDOWN_MS - 1), NOW)).toBe(false);
    expect(starPromptDue(shown(STAR_PROMPT_COOLDOWN_MS), NOW)).toBe(true);
  });

  it("stops after two showings or a star", () => {
    expect(starPromptDue(user({ star_prompt_shown_count: 2 }), NOW)).toBe(false);
    expect(starPromptDue(user({ star_prompt_starred_at: iso(NOW) }), NOW)).toBe(false);
  });
});

describe("claimStarPromptShowing", () => {
  it("records the granted showing on the signed-in user", async () => {
    authStore.setState({ user: user() });
    api.claimStarPromptShowing.mockResolvedValue(
      user({ star_prompt_shown_count: 1, star_prompt_last_shown_at: iso(NOW) }),
    );

    await expect(claimStarPromptShowing()).resolves.toBe(true);
    expect(authStore.getState().user?.star_prompt_shown_count).toBe(1);
  });

  it("reports a refused claim as not showable", async () => {
    authStore.setState({ user: user() });
    api.claimStarPromptShowing.mockRejectedValue(new Error("409"));

    await expect(claimStarPromptShowing()).resolves.toBe(false);
    expect(authStore.getState().user?.star_prompt_shown_count).toBe(0);
  });

  it("ignores a response that lands after an account switch", async () => {
    authStore.setState({ user: user() });
    api.claimStarPromptShowing.mockImplementation(async () => {
      authStore.setState({ user: user({ id: "u2" }) });
      return user({ star_prompt_shown_count: 1 });
    });

    await claimStarPromptShowing();
    expect(authStore.getState().user).toMatchObject({ id: "u2", star_prompt_shown_count: 0 });
  });
});

describe("markStarPromptStarred", () => {
  it("retires the prompt before the server answers", async () => {
    authStore.setState({ user: user() });
    let resolve!: (u: User) => void;
    api.markStarPromptStarred.mockReturnValue(new Promise<User>((r) => (resolve = r)));

    const pending = markStarPromptStarred();
    expect(starPromptDue(authStore.getState().user, NOW)).toBe(false);

    resolve(user({ star_prompt_starred_at: "2026-11-09T12:00:01Z" }));
    await pending;
    expect(authStore.getState().user?.star_prompt_starred_at).toBe("2026-11-09T12:00:01Z");
  });
});
