import { QueryClient } from "@tanstack/react-query";
import { describe, expect, it } from "vitest";
import { blockedReason, commandTitle, devCommands, searchCommands, type DevFeature } from "./actions";
import { DEV_COMMANDS, DEV_FEATURES } from "./registry";

const noop = () => {};
const features: DevFeature[] = [
  {
    id: "deck",
    group: "Prompts",
    title: "What's new deck",
    actions: [
      { id: "show", title: "Show deck", run: noop },
      {
        id: "seen",
        title: "Set seen version",
        choices: [
          { value: "", label: "none" },
          { value: "0.9.0", label: "0.9.0" },
        ],
        run: noop,
      },
    ],
  },
  { id: "errors", group: "Errors", title: "Error capture", actions: [{ id: "log", title: "Log a deck error", run: noop }] },
];

describe("devCommands", () => {
  it("makes each choice a command of its own", () => {
    expect(devCommands(features).map((c) => [c.key, commandTitle(c)])).toEqual([
      ["deck/show", "Show deck"],
      ["deck/seen=", "Set seen version → none"],
      ["deck/seen=0.9.0", "Set seen version → 0.9.0"],
      ["errors/log", "Log a deck error"],
    ]);
  });
});

describe("searchCommands", () => {
  const commands = devCommands(features);
  const keys = (query: string) => searchCommands(commands, query).map((c) => c.key);

  it("needs every word, anywhere in the command", () => {
    expect(keys("seen 0.9")).toEqual(["deck/seen=0.9.0"]);
    expect(keys("errors")).toEqual(["errors/log"]);
    expect(keys("prompts show")).toEqual(["deck/show"]);
    expect(keys("nothing like this")).toEqual([]);
    expect(keys("  ")).toEqual([]);
  });

  it("puts titles that start with the query first", () => {
    expect(keys("deck")).toEqual(["deck/show", "errors/log", "deck/seen=", "deck/seen=0.9.0"]);
    expect(keys("log")).toEqual(["errors/log"]);
  });
});

describe("blockedReason", () => {
  const ctx = { wsId: null, user: null, queryClient: new QueryClient() };
  it("says what an action still needs", () => {
    expect(blockedReason({ id: "a", title: "A", needs: "workspace", run: noop }, ctx)).toBe("needs a workspace");
    expect(blockedReason({ id: "a", title: "A", needs: "user", run: noop }, ctx)).toBe("needs a signed-in user");
    expect(blockedReason({ id: "a", title: "A", run: noop }, ctx)).toBeNull();
  });
});

describe("registry", () => {
  it("gives every feature and command its own key", () => {
    const featureIds = DEV_FEATURES.map((f) => f.id);
    expect(new Set(featureIds).size).toBe(featureIds.length);
    const keys = DEV_COMMANDS.map((c) => c.key);
    expect(new Set(keys).size).toBe(keys.length);
  });
});
