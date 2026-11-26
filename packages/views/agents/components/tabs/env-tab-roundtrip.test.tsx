// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ApiClient } from "@multica/core/api/client";
import { I18nProvider } from "@multica/core/i18n/react";
import type { Agent } from "@multica/core/types";
import common from "../../../locales/en/common.json";
import agents from "../../../locales/en/agents.json";

const api = vi.hoisted(() => ({
  getAgentEnv: vi.fn(),
  updateAgentEnv: vi.fn(),
}));
vi.mock("@multica/core/api", () => ({ api }));
vi.mock("sonner", () => ({ toast: { error: vi.fn(), success: vi.fn() } }));
import { EnvTab } from "./env-tab";

beforeEach(() => vi.clearAllMocks());
afterEach(() => vi.unstubAllGlobals());

it.each([
  "edit other",
  "no note",
  "old server",
  "null notes",
  "clear note",
  "rename",
  "delete",
  "bulk",
  "dotenv",
])(
  "round-trips special keys through ApiClient and EnvTab: %s",
  async (operation) => {
    let stored = JSON.parse(
      '{"agent_id":"a","custom_env":{"__proto__":"existing-value","constructor":"constructor-value","prototype":"prototype-value","SAFE":"safe-value"},"custom_env_notes":{"__proto__":"existing-note","constructor":"constructor-note","prototype":"prototype-note"}}',
    );
    if (operation === "no note") stored.custom_env_notes = {};
    if (operation === "old server") delete stored.custom_env_notes;
    if (operation === "null notes") stored.custom_env_notes = null;
    const requests: {
      custom_env: Record<string, string>;
      custom_env_notes?: Record<string, string>;
    }[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_url, options) => {
        if (options?.method === "PUT") {
          const body = JSON.parse(options.body);
          requests.push(body);
          stored = { agent_id: "a", ...body };
        }
        return new Response(JSON.stringify(stored), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }),
    );
    const client = new ApiClient("https://api.example.test");
    api.getAgentEnv.mockImplementation((id) => client.getAgentEnv(id));
    api.updateAgentEnv.mockImplementation((id, body) =>
      client.updateAgentEnv(id, body),
    );
    const agent = { id: "a", custom_env_key_count: 4 } as Agent;
    const view = render(
      <I18nProvider locale="en" resources={{ en: { common, agents } }}>
        <EnvTab agent={agent} />
      </I18nProvider>,
    );
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: /reveal & edit/i }));
    await screen.findByDisplayValue("__proto__");
    expect(screen.getAllByPlaceholderText("value")[0]).toHaveAttribute(
      "type",
      "password",
    );
    if (operation === "clear note") {
      await user.clear(
        screen.getByRole("textbox", {
          name: "Note for __proto__",
        }),
      );
    } else if (operation === "rename") {
      fireEvent.change(screen.getByDisplayValue("__proto__"), {
        target: { value: "RENAMED" },
      });
    } else if (operation === "delete") {
      await user.click(screen.getAllByRole("button", { name: /remove/i })[0]!);
    } else if (operation === "bulk") {
      await user.click(screen.getByRole("button", { name: /bulk edit/i }));
      fireEvent.change(screen.getByRole("textbox", { name: /bulk edit/i }), {
        target: {
          value:
            "__proto__=existing-value\nconstructor=constructor-value\nprototype=prototype-value\nSAFE=edited",
        },
      });
    } else if (operation === "dotenv") {
      fireEvent.paste(screen.getByDisplayValue("__proto__"), {
        clipboardData: { getData: () => "# not a note\n__proto__=edited" },
      });
    } else {
      fireEvent.change(screen.getAllByPlaceholderText("value")[3]!, {
        target: { value: "edited" },
      });
    }
    await user.click(screen.getByRole("button", { name: /^save$/i }));
    await waitFor(() =>
      expect(screen.getByRole("button", { name: /^save$/i })).toBeDisabled(),
    );
    expect(requests).toHaveLength(1);
    const saved = requests[0]!;
    expect(saved.custom_env.constructor).toBe("constructor-value");
    expect(saved.custom_env.prototype).toBe("prototype-value");
    if (operation === "rename" || operation === "delete") {
      expect(Object.hasOwn(saved.custom_env, "__proto__")).toBe(false);
      expect(Object.hasOwn(saved.custom_env_notes!, "__proto__")).toBe(false);
      if (operation === "rename") {
        expect(saved.custom_env.RENAMED).toBe("existing-value");
        expect(saved.custom_env_notes!.RENAMED).toBe("existing-note");
      }
    } else {
      expect(Object.hasOwn(saved.custom_env, "__proto__")).toBe(true);
      expect(saved.custom_env.__proto__).toBe(
        operation === "dotenv" ? "edited" : "existing-value",
      );
      if (["old server", "null notes"].includes(operation)) {
        expect(saved).not.toHaveProperty("custom_env_notes");
      } else {
        expect(Object.hasOwn(saved.custom_env_notes!, "__proto__")).toBe(
          !["no note", "clear note"].includes(operation),
        );
      }
    }
    expect(Object.hasOwn(Object.prototype, "SAFE")).toBe(false);
    // The PUT response must retain the active editor before another reveal.
    if (operation === "bulk") {
      expect(
        (
          screen.getByRole("textbox", {
            name: /bulk edit/i,
          }) as HTMLTextAreaElement
        ).value,
      ).toContain("__proto__=existing-value");
    } else {
      expect(
        screen
          .getAllByPlaceholderText("KEY")
          .map((input) => (input as HTMLInputElement).value),
      ).toEqual(Object.keys(saved.custom_env));
    }
    view.unmount();
    render(
      <I18nProvider locale="en" resources={{ en: { common, agents } }}>
        <EnvTab agent={agent} />
      </I18nProvider>,
    );
    await user.click(screen.getByRole("button", { name: /reveal & edit/i }));
    await screen.findByDisplayValue("constructor");
    expect(
      screen
        .getAllByPlaceholderText("KEY")
        .map((input) => (input as HTMLInputElement).value),
    ).toEqual(Object.keys(saved.custom_env));
  },
);
