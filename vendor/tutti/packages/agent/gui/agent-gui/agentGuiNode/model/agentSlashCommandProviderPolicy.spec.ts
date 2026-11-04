import { describe, expect, it } from "vitest";
import {
  resolveSlashCommandsForProvider,
  resolveSlashCommandSelectionEffect,
  resolveSlashCommandSubmitEffect
} from "./agentSlashCommandProviderPolicy";
import type { AgentSlashCommandPolicy } from "./agentSlashCommandProviderPolicy";

const CODEX_POLICY = {
  fallbackCommands: ["compact", "status", "fast", "goal", "review"],
  commandEffects: [
    { command: "init", effect: "submitImmediate" },
    { command: "compact", effect: "submitImmediate" },
    { command: "review", effect: "showReviewPicker" },
    { command: "plan", effect: "togglePlanMode" },
    { command: "status", effect: "showStatus" },
    { command: "fast", effect: "toggleSpeed" }
  ]
} as const;

const CLAUDE_POLICY = {
  fallbackCommands: ["compact", "status", "fast", "goal", "review"],
  commandCatalogAuthoritative: true,
  commandEffects: [
    { command: "compact", effect: "submitImmediate" },
    { command: "context", effect: "submitImmediate" },
    { command: "usage", effect: "submitImmediate" },
    { command: "review", effect: "showReviewPicker" },
    { command: "plan", effect: "togglePlanMode" },
    { command: "status", effect: "showStatus" },
    { command: "fast", effect: "toggleSpeed" }
  ]
} as const;

const CURSOR_POLICY = {
  fallbackCommands: ["plan"],
  commandCatalogAuthoritative: true,
  commandEffects: [{ command: "plan", effect: "togglePlanMode" }]
} as const;

const OPENCODE_POLICY = {
  fallbackCommands: ["compact", "goal", "review"],
  commandEffects: [
    { command: "compact", effect: "submitImmediate" },
    { command: "review", effect: "showReviewPicker" },
    { command: "plan", effect: "togglePlanMode" }
  ]
} as const;

describe("agentSlashCommandProviderPolicy", () => {
  const reviewPickerProviders = ["codex", "claude-code"] as const;

  it("surfaces the compatible Plan modifier as a slash entry", () => {
    const commands = resolveSlashCommandsForProvider({
      provider: "codex",
      policy: CODEX_POLICY,
      commands: [],
      planSupported: true
    });

    expect(commands.map((command) => command.name)).toEqual([
      "compact",
      "status",
      "fast",
      "goal",
      "review",
      "plan"
    ]);
    expect(
      resolveSlashCommandSelectionEffect({
        provider: "codex",
        policy: CODEX_POLICY,
        command: commands.find((command) => command.name === "plan")!,
        currentDraft: "/plan"
      })
    ).toEqual({ kind: "enablePlanMode" });
  });

  it("reserves the local Plan command when Plan is supported", () => {
    const commands = resolveSlashCommandsForProvider({
      provider: "codex",
      policy: CODEX_POLICY,
      commands: [{ name: "plan" }],
      planSupported: true
    });

    expect(commands.filter((command) => command.name === "plan")).toEqual([
      { name: "plan" }
    ]);
  });

  it("adds browser-use as a composer capability when browser use is supported", () => {
    const commands = resolveSlashCommandsForProvider({
      provider: "codex",
      policy: CODEX_POLICY,
      commands: [],
      browserSupported: true
    });

    expect(
      commands.find(
        (command) => "kind" in command && command.kind === "capability"
      )
    ).toEqual({
      kind: "capability",
      capability: "browserUse",
      name: "browser",
      aliases: ["浏览器"]
    });
  });

  it("keeps OpenCode descriptor commands beside browser capability entries", () => {
    expect(
      resolveSlashCommandsForProvider({
        provider: "opencode",
        policy: OPENCODE_POLICY,
        commands: [],
        planSupported: true,
        browserSupported: true
      }).map((command) => command.name)
    ).toEqual(["compact", "goal", "review", "plan", "browser"]);
  });

  it("fills a canonical browser token from Chinese and English slash capability names", () => {
    const commands = resolveSlashCommandsForProvider({
      provider: "codex",
      policy: CODEX_POLICY,
      commands: [],
      browserSupported: true
    });

    expect(
      resolveSlashCommandSelectionEffect({
        provider: "codex",
        policy: CODEX_POLICY,
        command: commands.find((command) => command.name === "browser")!,
        currentDraft: "/浏览"
      })
    ).toEqual({ kind: "enableBrowserUse", draft: "/browser " });
    expect(
      resolveSlashCommandSubmitEffect({
        provider: "codex",
        policy: CODEX_POLICY,
        commands,
        draft: "/浏览器",
        browserSupported: true
      })
    ).toEqual({
      kind: "submitPrompt",
      prompt: expect.stringContaining("browser-use"),
      displayPrompt: "/browser",
      requiredSettingsPatch: { browserUse: true }
    });
    expect(
      resolveSlashCommandSubmitEffect({
        provider: "extension-provider",
        browserSupported: true,
        commands,
        draft: "/browser打开百度"
      })
    ).toEqual({
      kind: "submitPrompt",
      prompt: expect.stringMatching(/browser-use[\s\S]*打开百度/),
      displayPrompt: "/browser 打开百度",
      requiredSettingsPatch: { browserUse: true }
    });
    expect(
      resolveSlashCommandSubmitEffect({
        provider: "codex",
        policy: CODEX_POLICY,
        browserSupported: true,
        commands,
        draft: "$browser 帮我访问下 google.com"
      })
    ).toEqual({
      kind: "submitPrompt",
      prompt: expect.stringContaining("帮我访问下 google.com"),
      displayPrompt: "/browser 帮我访问下 google.com",
      requiredSettingsPatch: { browserUse: true }
    });
    expect(
      resolveSlashCommandSubmitEffect({
        provider: "codex",
        policy: CODEX_POLICY,
        browserSupported: false,
        commands,
        draft: "$browser test"
      })
    ).toBeNull();
  });

  it("routes computer capability invocations through the local computer-use handoff", () => {
    const commands = resolveSlashCommandsForProvider({
      provider: "claude-code",
      policy: CLAUDE_POLICY,
      commands: [],
      computerSupported: true
    });

    expect(
      resolveSlashCommandSelectionEffect({
        provider: "claude-code",
        policy: CLAUDE_POLICY,
        command: commands.find((command) => command.name === "computer")!,
        currentDraft: "/comp"
      })
    ).toEqual({ kind: "enableComputerUse", draft: "/computer " });
    expect(
      resolveSlashCommandSubmitEffect({
        provider: "claude-code",
        policy: CLAUDE_POLICY,
        computerSupported: true,
        commands,
        draft: "/computer 你好"
      })
    ).toEqual({
      kind: "submitPrompt",
      prompt: expect.stringMatching(/computer-use[\s\S]*你好/),
      displayPrompt: "/computer 你好",
      requiredSettingsPatch: { computerUse: true }
    });
    expect(
      resolveSlashCommandSubmitEffect({
        provider: "claude-code",
        policy: CLAUDE_POLICY,
        computerSupported: true,
        commands,
        draft: "$电脑 点击确认"
      })
    ).toEqual({
      kind: "submitPrompt",
      prompt: expect.stringMatching(/computer-use[\s\S]*点击确认/),
      displayPrompt: "/computer 点击确认",
      requiredSettingsPatch: { computerUse: true }
    });
    expect(
      resolveSlashCommandSubmitEffect({
        provider: "claude-code",
        policy: CLAUDE_POLICY,
        computerSupported: false,
        commands,
        draft: "/computer test"
      })
    ).toBeNull();
  });

  it("adds Codex compact, status, fast, goal, and review fallback commands after provider commands", () => {
    expect(
      resolveSlashCommandsForProvider({
        provider: "codex",
        policy: CODEX_POLICY,
        commands: [{ name: "web" }, { name: "compact", description: "ACP" }]
      })
    ).toEqual([
      { name: "web" },
      { name: "compact", description: "ACP" },
      { name: "status" },
      { name: "fast" },
      { name: "goal" },
      { name: "review" }
    ]);
  });

  it("adds Claude Code fallback commands including goal when SDK commands are empty", () => {
    expect(
      resolveSlashCommandsForProvider({
        provider: "claude-code",
        policy: CLAUDE_POLICY,
        commands: []
      }).map((command) => command.name)
    ).toEqual(["compact", "status", "fast", "goal", "review"]);
  });

  it("keeps noisy Claude Code discovered commands out of the slash palette", () => {
    expect(
      resolveSlashCommandsForProvider({
        provider: "claude-code",
        policy: CLAUDE_POLICY,
        commands: [
          { name: "browser-use-tutti-10" },
          { name: "lark-mail" },
          { name: "context", description: "Show context" },
          { name: "goal" },
          { name: "security-review" },
          { name: "usage" }
        ]
      }).map((command) => command.name)
    ).toEqual([
      "context",
      "goal",
      "usage",
      "compact",
      "status",
      "fast",
      "review"
    ]);
  });

  it("trusts typed Claude Code policy commands instead of the legacy allowlist", () => {
    expect(
      resolveSlashCommandsForProvider({
        provider: "claude-code",
        policy: {
          fallbackCommands: ["custom-command"],
          commandCatalogAuthoritative: true,
          commandEffects: []
        },
        commands: [{ name: "descriptor-command" }]
      }).map((command) => command.name)
    ).toEqual(["custom-command"]);
  });

  it("filters compact when the session has no compactable context", () => {
    expect(
      resolveSlashCommandsForProvider({
        provider: "codex",
        policy: CODEX_POLICY,
        commands: [{ name: "compact", description: "from provider" }],
        hasCompactableContext: false
      })
    ).toEqual([
      { name: "status" },
      { name: "fast" },
      { name: "goal" },
      { name: "review" }
    ]);
  });

  it("filters Claude Code plan commands from provider and fallback commands", () => {
    expect(
      resolveSlashCommandsForProvider({
        provider: "claude-code",
        policy: CLAUDE_POLICY,
        commands: [{ name: "plan", description: "provider plan" }]
      })
    ).toEqual([
      { name: "compact" },
      { name: "status" },
      { name: "fast" },
      { name: "goal" },
      { name: "review" }
    ]);
  });

  it("submits Codex init and compact commands immediately", () => {
    expect(
      resolveSlashCommandSelectionEffect({
        provider: "codex",
        policy: CODEX_POLICY,
        command: { name: "init" },
        currentDraft: "/"
      })
    ).toEqual({ kind: "submitPrompt", prompt: "/init" });
    expect(
      resolveSlashCommandSelectionEffect({
        provider: "codex",
        policy: CODEX_POLICY,
        command: { name: " compact " },
        currentDraft: "/"
      })
    ).toEqual({ kind: "submitPrompt", prompt: "/compact" });
  });

  it("does not apply legacy immediate-command behavior over a typed policy", () => {
    expect(
      resolveSlashCommandSelectionEffect({
        provider: "codex",
        policy: { fallbackCommands: ["compact"], commandEffects: [] },
        command: { name: "compact" },
        currentDraft: "/"
      })
    ).toEqual({ kind: "fillDraft", draft: "/compact " });
  });

  it("handles Codex local status without provider prompts", () => {
    expect(
      resolveSlashCommandSelectionEffect({
        provider: "codex",
        policy: CODEX_POLICY,
        command: { name: "status" },
        currentDraft: "/sta"
      })
    ).toEqual({ kind: "showStatus" });
    expect(
      resolveSlashCommandSelectionEffect({
        provider: "codex",
        policy: CODEX_POLICY,
        command: { name: "plan" },
        currentDraft: "/pla"
      })
    ).toEqual({ kind: "enablePlanMode" });
  });

  it("handles Claude Code local status without provider prompts", () => {
    expect(
      resolveSlashCommandSelectionEffect({
        provider: "claude-code",
        policy: CLAUDE_POLICY,
        command: { name: "status" },
        currentDraft: "/sta"
      })
    ).toEqual({ kind: "showStatus" });
    expect(
      resolveSlashCommandSelectionEffect({
        provider: "claude-code",
        policy: CLAUDE_POLICY,
        command: { name: "plan" },
        currentDraft: "/pla"
      })
    ).toEqual({ kind: "enablePlanMode" });
  });

  it("submits advertised Claude Code compact command as provider-native prompt", () => {
    expect(
      resolveSlashCommandSelectionEffect({
        provider: "claude-code",
        policy: CLAUDE_POLICY,
        command: { name: "compact" },
        currentDraft: "/"
      })
    ).toEqual({ kind: "submitPrompt", prompt: "/compact" });
  });

  it("parses manual Codex status and toggles plan mode on submit", () => {
    const commands = resolveSlashCommandsForProvider({
      provider: "codex",
      policy: CODEX_POLICY,
      commands: [],
      planSupported: true
    });

    expect(
      resolveSlashCommandSubmitEffect({
        provider: "codex",
        policy: CODEX_POLICY,
        commands,
        draft: "/status"
      })
    ).toEqual({ kind: "showStatus" });
    expect(
      resolveSlashCommandSubmitEffect({
        provider: "codex",
        policy: CODEX_POLICY,
        commands,
        draft: "/plan"
      })
    ).toEqual({ kind: "enablePlanMode" });
  });

  it("parses manual Claude Code status and toggles plan mode on submit", () => {
    const commands = resolveSlashCommandsForProvider({
      provider: "claude-code",
      policy: CLAUDE_POLICY,
      commands: [],
      planSupported: true
    });

    expect(
      resolveSlashCommandSubmitEffect({
        provider: "claude-code",
        policy: CLAUDE_POLICY,
        commands,
        draft: "/status"
      })
    ).toEqual({ kind: "showStatus" });
    expect(
      resolveSlashCommandSubmitEffect({
        provider: "claude-code",
        policy: CLAUDE_POLICY,
        commands,
        draft: "/plan"
      })
    ).toEqual({ kind: "enablePlanMode" });
    expect(
      resolveSlashCommandSubmitEffect({
        provider: "claude-code",
        policy: CLAUDE_POLICY,
        commands,
        draft: "/plan refactor auth"
      })
    ).toBeNull();
  });

  it("surfaces /plan only when plan mode is supported", () => {
    expect(
      resolveSlashCommandsForProvider({
        provider: "codex",
        policy: CODEX_POLICY,
        commands: [{ name: "plan" }],
        planSupported: false
      }).some((command) => command.name === "plan")
    ).toBe(false);
    expect(
      resolveSlashCommandsForProvider({
        provider: "codex",
        policy: CODEX_POLICY,
        commands: [],
        planSupported: true
      }).filter((command) => command.name === "plan")
    ).toHaveLength(1);
  });

  it("surfaces only /plan for Cursor and hides agent-advertised slash commands", () => {
    expect(
      resolveSlashCommandsForProvider({
        provider: "cursor",
        policy: CURSOR_POLICY,
        commands: [
          { name: "compact" },
          { name: "goal" },
          { name: "review" },
          { name: "browser-use-tutti-10" }
        ],
        planSupported: true
      }).map((command) => command.name)
    ).toEqual(["plan"]);
    expect(
      resolveSlashCommandsForProvider({
        provider: "cursor",
        policy: CURSOR_POLICY,
        commands: [{ name: "plan", description: "provider plan" }],
        planSupported: false
      })
    ).toEqual([]);
  });

  it("keeps runtime commands provider-native when a descriptor policy is missing", () => {
    expect(
      resolveSlashCommandsForProvider({
        provider: "cursor",
        commands: [{ name: "plan" }, { name: "compact" }],
        planSupported: true
      })
    ).toEqual([{ name: "plan" }, { name: "compact" }]);
    expect(
      resolveSlashCommandSelectionEffect({
        provider: "cursor",
        command: { name: "plan" },
        currentDraft: "/"
      })
    ).toEqual({ kind: "fillDraft", draft: "/plan " });
  });

  it("shows open-provider runtime commands without a descriptor policy", () => {
    expect(
      resolveSlashCommandsForProvider({
        provider: "other-provider",
        commands: [{ name: "compact" }]
      })
    ).toEqual([{ name: "compact" }]);
  });

  it("shows extension fallback commands when command effects are absent", () => {
    const policy = {
      fallbackCommands: ["compact", "status"],
      commandCatalogAuthoritative: true
    } as unknown as AgentSlashCommandPolicy;

    expect(
      resolveSlashCommandsForProvider({
        provider: "acp:hermes",
        policy,
        commands: []
      })
    ).toEqual([{ name: "compact" }, { name: "status" }]);
  });

  it("keeps non-plan advertised Claude Code controls provider-native", () => {
    expect(
      resolveSlashCommandSubmitEffect({
        provider: "claude-code",
        policy: CLAUDE_POLICY,
        commands: resolveSlashCommandsForProvider({
          provider: "claude-code",
          policy: CLAUDE_POLICY,
          commands: [{ name: "context", description: "provider context" }]
        }),
        draft: "/context"
      })
    ).toEqual({ kind: "submitPrompt", prompt: "/context" });
  });

  it("does not intercept unknown slash text", () => {
    expect(
      resolveSlashCommandSubmitEffect({
        provider: "codex",
        policy: CODEX_POLICY,
        commands: resolveSlashCommandsForProvider({
          provider: "codex",
          policy: CODEX_POLICY,
          commands: []
        }),
        draft: "/unknown"
      })
    ).toBeNull();
  });

  it.each(reviewPickerProviders)(
    "opens the review picker when picking %s /review from the palette",
    (provider) => {
      expect(
        resolveSlashCommandSelectionEffect({
          provider,

          policy: provider === "codex" ? CODEX_POLICY : CLAUDE_POLICY,
          command: { name: "review", description: "Review code changes" },
          currentDraft: "/rev"
        })
      ).toEqual({ kind: "showReviewPicker" });
    }
  );

  it.each(reviewPickerProviders)(
    "opens the review picker when submitting bare /review on %s",
    (provider) => {
      const commands = resolveSlashCommandsForProvider({
        provider,

        policy: provider === "codex" ? CODEX_POLICY : CLAUDE_POLICY,
        commands: [{ name: "review", description: "Review code changes" }]
      });
      expect(
        resolveSlashCommandSubmitEffect({
          provider,

          policy: provider === "codex" ? CODEX_POLICY : CLAUDE_POLICY,
          commands,
          draft: "/review"
        })
      ).toEqual({ kind: "showReviewPicker" });
    }
  );

  it.each(reviewPickerProviders)(
    "opens the review picker from %s fallback /review before provider commands arrive",
    (provider) => {
      const commands = resolveSlashCommandsForProvider({
        provider,

        policy: provider === "codex" ? CODEX_POLICY : CLAUDE_POLICY,
        commands: [],
        hasCompactableContext: false
      });
      expect(commands.map((command) => command.name)).toContain("review");
      expect(
        resolveSlashCommandSubmitEffect({
          provider,

          policy: provider === "codex" ? CODEX_POLICY : CLAUDE_POLICY,
          commands,
          draft: "/review"
        })
      ).toEqual({ kind: "showReviewPicker" });
    }
  );

  it.each(reviewPickerProviders)(
    "submits /review <text> straight through as a %s custom review",
    (provider) => {
      const commands = resolveSlashCommandsForProvider({
        provider,

        policy: provider === "codex" ? CODEX_POLICY : CLAUDE_POLICY,
        commands: [{ name: "review", description: "Review code changes" }]
      });
      expect(
        resolveSlashCommandSubmitEffect({
          provider,

          policy: provider === "codex" ? CODEX_POLICY : CLAUDE_POLICY,
          commands,
          draft: "/review check the auth flow"
        })
      ).toBeNull();
    }
  );

  it("keeps review provider-native for unknown providers", () => {
    expect(
      resolveSlashCommandSelectionEffect({
        provider: "other-provider",
        command: { name: "review", description: "Review" },
        currentDraft: "/rev"
      })
    ).toEqual({ kind: "fillDraft", draft: "/review " });
  });
});

describe("compact capability gating", () => {
  const policy = {
    commandCatalogAuthoritative: true,
    commandEffects: [],
    fallbackCommands: ["compact", "status"]
  };

  it("keeps descriptor compact when capability is unknown", () => {
    const commands = resolveSlashCommandsForProvider({
      provider: "codex",
      commands: [{ name: "compact" }, { name: "status" }],
      policy
    });
    expect(commands.some((command) => command.name === "compact")).toBe(true);
  });

  it("keeps compact when capability resolves true", () => {
    const commands = resolveSlashCommandsForProvider({
      provider: "codex",
      commands: [{ name: "compact" }],
      compactSupported: true,
      policy
    });
    expect(commands.some((command) => command.name === "compact")).toBe(true);
  });

  it("drops compact (including fallback) when capability resolves false", () => {
    const commands = resolveSlashCommandsForProvider({
      provider: "codex",
      commands: [{ name: "compact" }, { name: "status" }],
      compactSupported: false,
      policy
    });
    expect(commands.some((command) => command.name === "compact")).toBe(false);
    expect(commands.some((command) => command.name === "status")).toBe(true);
  });
});
