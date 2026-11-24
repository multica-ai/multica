// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  LABEL_TEXT_COLORS,
  PALETTE_PRESETS,
  PALETTE_SLOTS,
} from "@multica/core/appearance";
import {
  TEXT_AVATAR_COLORS,
  TEXT_AVATAR_DEFAULT_COLOR,
  TEXT_AVATAR_SIZE,
  defaultAvatarText,
  normalizeAvatarText,
  renderTextAvatar,
  textAvatarTextColor,
} from "./text-avatar";

const [DARK_TEXT, LIGHT_TEXT] = LABEL_TEXT_COLORS;

describe("text icon colors", () => {
  it("offers the app's palette presets, in the picker's order", () => {
    expect(TEXT_AVATAR_COLORS).toEqual(
      PALETTE_SLOTS.map((slot) => PALETTE_PRESETS[slot]),
    );
    expect(TEXT_AVATAR_COLORS).toHaveLength(10);
    expect(TEXT_AVATAR_DEFAULT_COLOR).toBe(PALETTE_PRESETS.blue);
  });

  it("picks dark text on light presets and light text on dark ones", () => {
    expect(textAvatarTextColor(PALETTE_PRESETS.yellow)).toBe(DARK_TEXT);
    expect(textAvatarTextColor(PALETTE_PRESETS.orange)).toBe(DARK_TEXT);
    expect(textAvatarTextColor(PALETTE_PRESETS.blue)).toBe(LIGHT_TEXT);
    expect(textAvatarTextColor(PALETTE_PRESETS.gray)).toBe(LIGHT_TEXT);
  });
});

describe("normalizeAvatarText", () => {
  it("keeps up to two characters and cuts the rest", () => {
    expect(normalizeAvatarText("A")).toBe("A");
    expect(normalizeAvatarText("A3")).toBe("A3");
    expect(normalizeAvatarText("ABC")).toBe("AB");
    expect(normalizeAvatarText("研发三部")).toBe("研发");
  });

  it("drops whitespace and keeps the typed case", () => {
    expect(normalizeAvatarText("  a b  ")).toBe("ab");
    expect(normalizeAvatarText("xY")).toBe("xY");
  });

  it("is empty when nothing but whitespace was typed", () => {
    expect(normalizeAvatarText("")).toBe("");
    expect(normalizeAvatarText("   ")).toBe("");
  });

  it("counts a multi-code-point character as one", () => {
    expect(normalizeAvatarText("👍🏽👍🏽👍🏽")).toBe("👍🏽👍🏽");
  });
});

describe("defaultAvatarText", () => {
  it("takes the name's first character, upper-cased", () => {
    expect(defaultAvatarText("acme-a")).toBe("A");
    expect(defaultAvatarText("  研发")).toBe("研");
    expect(defaultAvatarText("")).toBe("");
  });
});

describe("renderTextAvatar", () => {
  const ctx = {
    fillStyle: "",
    font: "",
    textAlign: "",
    textBaseline: "",
    fillRect: vi.fn(),
    fillText: vi.fn(),
    measureText: vi.fn(() => ({
      width: 100,
      actualBoundingBoxAscent: 40,
      actualBoundingBoxDescent: 0,
    })),
  };
  const canvases: HTMLCanvasElement[] = [];

  function stubCanvas() {
    const createElement = document.createElement.bind(document);
    vi.spyOn(document, "createElement").mockImplementation(
      (tag: string, options?: ElementCreationOptions) => {
        const el = createElement(tag, options);
        if (tag === "canvas") {
          const canvas = el as HTMLCanvasElement;
          canvases.push(canvas);
          canvas.getContext = vi.fn(() => ctx) as never;
          canvas.toBlob = ((cb: BlobCallback, type?: string) =>
            cb(new Blob(["png"], { type }))) as never;
        }
        return el;
      },
    );
  }

  afterEach(() => {
    vi.restoreAllMocks();
    canvases.length = 0;
    ctx.fillRect.mockClear();
    ctx.fillText.mockClear();
  });

  it("produces a square PNG file of the fixed size", async () => {
    stubCanvas();
    const file = await renderTextAvatar("A3", "#2563eb");

    expect(file).toBeInstanceOf(File);
    expect(file.type).toBe("image/png");
    expect(file.name).toBe("workspace-icon.png");
    expect(canvases).toHaveLength(1);
    expect(canvases[0]!.width).toBe(TEXT_AVATAR_SIZE);
    expect(canvases[0]!.height).toBe(TEXT_AVATAR_SIZE);
    expect(TEXT_AVATAR_SIZE).toBe(256);
    expect(ctx.fillRect).toHaveBeenCalledWith(0, 0, 256, 256);
  });

  it("draws only the first two characters", async () => {
    stubCanvas();
    await renderTextAvatar("ABC", "#2563eb");

    expect(ctx.fillText).toHaveBeenCalledTimes(1);
    expect(ctx.fillText.mock.calls[0]![0]).toBe("AB");
  });

  it("shrinks text that would overflow the square", async () => {
    stubCanvas();
    ctx.measureText.mockReturnValueOnce({
      width: 400,
      actualBoundingBoxAscent: 40,
      actualBoundingBoxDescent: 0,
    });
    await renderTextAvatar("WW", "#2563eb");

    // 0.42 * 256 = 108px, scaled by 0.8 * 256 / 400.
    expect(ctx.font).toMatch(/^600 55px /);
  });

  it("fills the chosen color and draws text in the color that reads on it", async () => {
    stubCanvas();
    const styles: string[] = [];
    ctx.fillRect.mockImplementationOnce(() => styles.push(ctx.fillStyle));
    ctx.fillText.mockImplementationOnce(() => styles.push(ctx.fillStyle));
    await renderTextAvatar("A3", PALETTE_PRESETS.yellow);

    expect(styles).toEqual([PALETTE_PRESETS.yellow, DARK_TEXT]);
  });

  it("refuses empty text", async () => {
    stubCanvas();
    await expect(renderTextAvatar("  ", "#2563eb")).rejects.toThrow();
    expect(canvases).toHaveLength(0);
  });
});
