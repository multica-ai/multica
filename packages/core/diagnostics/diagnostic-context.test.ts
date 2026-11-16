import { afterEach, describe, expect, it } from "vitest";

import {
  bucketDiagnosticPath,
  getDiagnosticRoute,
  resetDiagnosticContext,
  setDiagnosticRoute,
} from "./diagnostic-context";
import { paths } from "../paths";

afterEach(() => {
  resetDiagnosticContext();
});

describe("diagnostic route", () => {
  it("holds the published route", () => {
    setDiagnosticRoute("/:slug/issues");
    expect(getDiagnosticRoute()).toBe("/:slug/issues");
  });

  it("treats empty and whitespace values as absent", () => {
    setDiagnosticRoute("   ");
    expect(getDiagnosticRoute()).toBeNull();
  });

  it("clears on null, so a window that leaves a route stops claiming it", () => {
    setDiagnosticRoute("/:slug/issues");
    setDiagnosticRoute(null);
    expect(getDiagnosticRoute()).toBeNull();
  });
});

describe("bucketDiagnosticPath", () => {
  it("replaces the workspace slug", () => {
    expect(bucketDiagnosticPath("/acme/issues")).toBe("/:slug/issues");
  });

  it("templates every workspace detail route", () => {
    expect(bucketDiagnosticPath("/acme/issues/MUL-5345")).toBe("/:slug/issues/:id");
    expect(bucketDiagnosticPath("/acme/projects/p1")).toBe("/:slug/projects/:id");
    expect(bucketDiagnosticPath("/acme/autopilots/ap-7")).toBe("/:slug/autopilots/:id");
    expect(bucketDiagnosticPath("/acme/agents/agt_9")).toBe("/:slug/agents/:id");
    expect(bucketDiagnosticPath("/acme/members/m-3")).toBe("/:slug/members/:id");
    expect(bucketDiagnosticPath("/acme/squads/sq.4")).toBe("/:slug/squads/:id");
    expect(bucketDiagnosticPath("/acme/runtimes/machine-1")).toBe("/:slug/runtimes/:id");
    expect(bucketDiagnosticPath("/acme/skills/skl_123")).toBe("/:slug/skills/:id");
    expect(bucketDiagnosticPath("/acme/attachments/att-8/preview")).toBe(
      "/:slug/attachments/:id/preview",
    );
    expect(bucketDiagnosticPath("/acme/runtimes/machine-1/runtime/rt-2")).toBe(
      "/:slug/runtimes/:id/runtime/:runtimeId",
    );
  });

  // The previous implementation guessed from the shape of a segment, so any id
  // that was not a UUID, an issue key or digits travelled to telemetry intact.
  it("templates ids that look nothing like ids", () => {
    for (const [path, expected] of [
      ["/acme/projects/p1", "/:slug/projects/:id"],
      ["/acme/skills/skl_123", "/:slug/skills/:id"],
      ["/acme/agents/my-favourite-agent", "/:slug/agents/:id"],
      ["/acme/squads/Platform Team", "/:slug/squads/:id"],
      ["/acme/issues/new", "/:slug/issues/:id"],
    ] as const) {
      expect(bucketDiagnosticPath(path)).toBe(expected);
    }
  });

  // paths.ts URL-encodes every id, so an id containing a slash or a space
  // arrives as one already-escaped segment.
  it("templates percent-encoded ids without splitting them", () => {
    expect(bucketDiagnosticPath("/acme/runtimes/machine%2Fruntime")).toBe(
      "/:slug/runtimes/:id",
    );
    expect(
      bucketDiagnosticPath("/acme/runtimes/machine%2Fruntime/runtime/runtime%20one"),
    ).toBe("/:slug/runtimes/:id/runtime/:runtimeId");
    expect(bucketDiagnosticPath("/my%20workspace/skills/a%2Fb")).toBe(
      "/:slug/skills/:id",
    );
  });

  it("keeps static segments that share a slot with an id", () => {
    expect(bucketDiagnosticPath("/acme/agents/new")).toBe("/:slug/agents/new");
    expect(bucketDiagnosticPath("/acme/agents")).toBe("/:slug/agents");
  });

  it("keeps pre-workspace routes intact", () => {
    expect(bucketDiagnosticPath("/login")).toBe("/login");
    expect(bucketDiagnosticPath("/workspaces/new")).toBe("/workspaces/new");
    expect(bucketDiagnosticPath("/invitations")).toBe("/invitations");
    expect(bucketDiagnosticPath("/auth/callback")).toBe("/auth/callback");
  });

  it("templates the invitation id", () => {
    expect(bucketDiagnosticPath("/invite/8db920bc-f982-4d38-95f3-56ec43cbefa8")).toBe(
      "/invite/:id",
    );
    expect(bucketDiagnosticPath("/invite/plain-token")).toBe("/invite/:id");
  });

  it("drops query string and hash — they can carry resource ids", () => {
    expect(bucketDiagnosticPath("/acme/issues?issue=MUL-1#comment-3")).toBe(
      "/:slug/issues",
    );
  });

  it("normalizes the root and a bare workspace path", () => {
    expect(bucketDiagnosticPath("/")).toBe("/");
    expect(bucketDiagnosticPath("/acme")).toBe("/:slug");
  });

  // A route we do not know is exactly the case where an id cannot be told from
  // a page name, so nothing from it travels.
  it("masks an unknown route instead of passing segments through", () => {
    expect(bucketDiagnosticPath("/acme/issues/MUL-1/secret-tab")).toBe(
      "/:slug/issues/*",
    );
    expect(bucketDiagnosticPath("/acme/not-a-section/raw-value")).toBe("/:slug/*");
    expect(bucketDiagnosticPath("/login/raw-value")).toBe("/login/*");
  });
});

// Bucketing is only safe while it knows every route. This walks the real path
// builders rather than a copy of them, so adding a route to paths.ts without
// adding it here fails CI instead of shipping a raw id.
describe("route coverage stays in step with paths.ts", () => {
  const SLUG = "workspace-slug-value";
  const RAW_ID = "raw-identifier-value";
  const RAW_SECOND_ID = "second-raw-identifier-value";

  function buildAll(): string[] {
    const scoped = paths.workspace(SLUG) as Record<string, (...args: string[]) => string>;
    const global = {
      login: paths.login,
      newWorkspace: paths.newWorkspace,
      invite: paths.invite,
      invitations: paths.invitations,
      onboarding: paths.onboarding,
      authCallback: paths.authCallback,
      root: paths.root,
    } as Record<string, (...args: string[]) => string>;

    return [...Object.values(scoped), ...Object.values(global)].map((build) =>
      build(RAW_ID, RAW_SECOND_ID),
    );
  }

  it("never lets a slug or an id through, for any builder", () => {
    for (const built of buildAll()) {
      const bucketed = bucketDiagnosticPath(built);
      expect(bucketed, `leaked from ${built}`).not.toContain(SLUG);
      expect(bucketed, `leaked from ${built}`).not.toContain(RAW_ID);
      expect(bucketed, `leaked from ${built}`).not.toContain(RAW_SECOND_ID);
    }
  });

  it("recognizes every builder — no builder falls back to the mask", () => {
    for (const built of buildAll()) {
      expect(bucketDiagnosticPath(built), `unrecognized: ${built}`).not.toContain("*");
    }
  });
});
