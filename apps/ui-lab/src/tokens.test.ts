// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  baseline,
  changeCount,
  editHistory,
  emptyDraft,
  exportCss,
  isDraft,
  parseColor,
  previewCss,
  readSourceTokens,
  sizeValue,
  updateToken,
} from "./tokens";
import { decodeSession, encodeSession } from "./storage";
import { isPreviewSettings } from "./protocol";

describe("the shared design-token contract", () => {
  it("reads current defaults and retains semantic aliases", () => {
    expect(baseline.light["--background"]).toBe("var(--page-canvas)");
    expect(sizeValue(baseline.shared["--radius"]!)).toBeGreaterThanOrEqual(0);
    expect(sizeValue(baseline.shared["--issue-row-height"]!)).toBeGreaterThan(
      0,
    );
    expect(parseColor(baseline.dark["--brand"]!)).toHaveLength(3);
    expect(changeCount(emptyDraft())).toBe(0);
    expect(previewCss(emptyDraft())).toBe("");
  });
  it("parses source blocks without mistaking comment examples for declarations", () => {
    const parsed = readSourceTokens(`
      /* :root { --radius: 999px; } */
      @theme { --text-body: 14px; }
      :root { --radius: 0.625rem; --issue-row-height: 36px; --background: var(--page-canvas); }
      .dark { --page-canvas: oklch(0.2 0 0); }
    `);
    expect(sizeValue(parsed.shared["--radius"]!)).toBe(10);
    expect(parsed.dark["--background"]).toBe("var(--page-canvas)");
  });
  it("keeps light, dark and shared changes separate in exported source", () => {
    let draft = updateToken(
      emptyDraft(),
      "light",
      "--brand",
      "oklch(0.5 0.12 150)",
    );
    draft = updateToken(draft, "dark", "--brand", "oklch(0.7 0.12 150)");
    draft = updateToken(draft, "shared", "--text-body", "16px");
    draft = updateToken(draft, "shared", "--issue-row-height", "40px");
    const css = exportCss(draft);
    expect(css).toContain("@theme {\n  --text-body: 16px;\n}");
    expect(css).toContain(
      ":root {\n  --brand: oklch(0.5 0.12 150);\n  --issue-row-height: 40px;\n}",
    );
    expect(css).toContain(".dark {\n  --brand: oklch(0.7 0.12 150);\n}");
    expect(css).not.toContain("--background:");
    expect(previewCss(draft)).not.toContain("@theme");
    expect(previewCss(draft)).toContain("--text-body: 16px");
  });
  it("removes a restored value even if px and rem differ", () => {
    const draft = updateToken(emptyDraft(), "shared", "--radius", "12px");
    expect(
      changeCount(
        updateToken(
          draft,
          "shared",
          "--radius",
          `${sizeValue(baseline.shared["--radius"]!)}px`,
        ),
      ),
    ).toBe(0);
  });
  it("rejects arbitrary CSS and malformed persisted values", () => {
    expect(
      isDraft({
        ...emptyDraft(),
        light: { "--brand": "red; } body { display: none" },
      }),
    ).toBe(false);
    expect(isDraft({ ...emptyDraft(), shared: { "--radius": "100px" } })).toBe(
      false,
    );
    expect(isDraft({ ...emptyDraft(), shared: { "--unknown": "12px" } })).toBe(
      false,
    );
    expect(
      isDraft({ ...emptyDraft(), dark: { "--brand": "oklch(. 0 200)" } }),
    ).toBe(false);
    expect(isDraft({ ...emptyDraft(), shared: [] })).toBe(false);
    expect(
      isDraft(updateToken(emptyDraft(), "shared", "--text-body", "16px")),
    ).toBe(true);
  });
});
describe("design editing and persistence", () => {
  it("undoes, redoes and discards the redo branch after a new edit", () => {
    const initial = { past: [], present: emptyDraft(), future: [] };
    const draft = updateToken(initial.present, "shared", "--radius", "12px");
    const edited = editHistory(initial, { type: "edit", draft });
    const undone = editHistory(edited, { type: "undo" });
    expect(undone.present).toEqual(initial.present);
    expect(editHistory(undone, { type: "redo" }).present).toEqual(draft);
    const branched = editHistory(undone, {
      type: "edit",
      draft: updateToken(emptyDraft(), "shared", "--radius", "14px"),
    });
    expect(branched.future).toEqual([]);
  });
  it("round trips saved schemes and ignores corrupted storage", () => {
    const draft = updateToken(
      emptyDraft(),
      "dark",
      "--brand",
      "oklch(0.7 0.1 200)",
    );
    const session = {
      draft,
      designs: [
        { id: "a", name: "Design A", draft, savedAt: "2026-09-14T00:00:00Z" },
      ],
    };
    expect(decodeSession(encodeSession(session))).toEqual(session);
    expect(decodeSession("{not json")).toEqual({
      draft: emptyDraft(),
      designs: [],
    });
    expect(
      decodeSession('{"version":1,"draft":null,"designs":[]}').designs,
    ).toEqual([]);
  });
  it("keeps saved overrides when the source revision changes", () => {
    const draft = updateToken(emptyDraft(), "shared", "--radius", "12px");
    const data = JSON.parse(encodeSession({ draft, designs: [] }));
    data.sourceRevision = "older-source";
    expect(decodeSession(JSON.stringify(data)).draft).toEqual(draft);
  });
  it("validates frame settings before applying CSS", () => {
    const message = {
      type: "multica-ui-lab:preview",
      selectedColor: "--brand",
      playbackSpeed: 1,
      locale: "en",
      theme: "dark",
      scene: "list",
      buttonScale: "default",
      draft: emptyDraft(),
    };
    expect(isPreviewSettings(message)).toBe(true);
    expect(isPreviewSettings({ ...message, scene: "untrusted" })).toBe(false);
    expect(isPreviewSettings({ ...message, draft: null })).toBe(false);
  });
});

describe("Button geometry export", () => {
  it("exports button dimensions to :root, with typography in @theme", () => {
    let draft = updateToken(
      emptyDraft(),
      "shared",
      "--button-height-default",
      "40px",
    );
    draft = updateToken(draft, "shared", "--button-padding-sm", "12px");
    draft = updateToken(draft, "shared", "--text-body", "16px");
    expect(isDraft(draft)).toBe(true);
    const css = exportCss(draft);
    expect(css).toContain(
      ":root {\n  --button-height-default: 40px;\n  --button-padding-sm: 12px;\n}",
    );
    expect(css).toContain("@theme {\n  --text-body: 16px;\n}");
    expect(decodeSession(encodeSession({ draft, designs: [] })).draft).toEqual(
      draft,
    );
    expect(previewCss(draft)).toContain("--button-height-default: 40px");
  });
  it("restores a button dimension to its source value and validates bounds", () => {
    const original = baseline.shared["--button-height-default"]!;
    const draft = updateToken(
      emptyDraft(),
      "shared",
      "--button-height-default",
      "40px",
    );
    expect(
      updateToken(
        draft,
        "shared",
        "--button-height-default",
        `${sizeValue(original)}px`,
      ),
    ).toEqual(emptyDraft());
    expect(
      isDraft({ ...draft, shared: { "--button-height-default": "200px" } }),
    ).toBe(false);
    expect(isDraft({ ...draft, shared: { "--button-gap-lg": "-2px" } })).toBe(
      false,
    );
    expect(
      isDraft({ ...emptyDraft(), dark: { "--button-height-default": "40px" } }),
    ).toBe(false);
  });
  it("accepts only supported button preview sizes", () => {
    const message = {
      type: "multica-ui-lab:preview",
      selectedColor: "--brand",
      playbackSpeed: 1,
      locale: "en",
      theme: "light",
      scene: "button",
      buttonScale: "sm",
      draft: emptyDraft(),
    };
    expect(isPreviewSettings(message)).toBe(true);
    expect(isPreviewSettings({ ...message, buttonScale: "huge" })).toBe(false);
  });
});

describe("preview theme isolation", () => {
  it("limits light overrides to the light root while keeping geometry shared", () => {
    const draft = {
      light: { "--primary": "oklch(0.5 0.2 260)" },
      dark: {},
      shared: { "--button-height-default": "40px" },
    };
    const css = previewCss(draft);
    expect(css).toContain(":root {\n  --button-height-default: 40px;\n}");
    expect(css).toContain(
      ":root:not(.dark) {\n  --primary: oklch(0.5 0.2 260);\n}",
    );
    expect(css).not.toContain(".dark {");
  });
});

describe("Dialog motion token contract", () => {
  it("preserves default timing and round trips edited motion through history, persistence and export", () => {
    expect(baseline.shared["--dialog-enter-duration"]).toBe("100ms");
    expect(baseline.shared["--dialog-exit-duration"]).toBe("100ms");
    expect(baseline.shared["--dialog-enter-easing"]).toBe("ease");
    let draft = updateToken(
      emptyDraft(),
      "shared",
      "--dialog-enter-duration",
      "320ms",
    );
    draft = updateToken(draft, "shared", "--dialog-exit-duration", "0ms");
    draft = updateToken(
      draft,
      "shared",
      "--dialog-enter-easing",
      "cubic-bezier(0.23, 1, 0.32, 1)",
    );
    expect(isDraft(draft)).toBe(true);
    expect(decodeSession(encodeSession({ draft, designs: [] })).draft).toEqual(
      draft,
    );
    const css = exportCss(draft);
    expect(css).toContain("--dialog-enter-duration: 320ms;");
    expect(css).toContain("--dialog-exit-duration: 0ms;");
    expect(css).toContain(
      "--dialog-enter-easing: cubic-bezier(0.23, 1, 0.32, 1);",
    );
    expect(css).not.toContain("@theme");
    const history = editHistory(
      { past: [], present: emptyDraft(), future: [] },
      { type: "edit", draft },
    );
    expect(editHistory(history, { type: "undo" }).present).toEqual(
      emptyDraft(),
    );
    expect(
      editHistory(editHistory(history, { type: "undo" }), { type: "redo" })
        .present,
    ).toEqual(draft);
    expect(
      updateToken(draft, "shared", "--dialog-enter-duration", "100ms").shared,
    ).not.toHaveProperty("--dialog-enter-duration");
  });
  it("rejects incorrect units, unbounded timings and arbitrary easing CSS", () => {
    for (const value of [
      "100px",
      "1s",
      "-1ms",
      "1001ms",
      "NaNms",
      "100ms; color: red",
      "var(--other)",
    ]) {
      expect(
        isDraft({
          ...emptyDraft(),
          shared: { "--dialog-enter-duration": value },
        }),
        value,
      ).toBe(false);
    }
    for (const value of [
      "cubic-bezier(2, 0, 1, 1)",
      "ease; display: none",
      "var(--unknown)",
    ]) {
      expect(
        isDraft({
          ...emptyDraft(),
          shared: { "--dialog-enter-easing": value },
        }),
        value,
      ).toBe(false);
    }
    expect(isDraft({ ...emptyDraft(), shared: { "--radius": "10ms" } })).toBe(
      false,
    );
    expect(
      isDraft({
        ...emptyDraft(),
        dark: { "--dialog-enter-duration": "200ms" },
      }),
    ).toBe(false);
  });
});
