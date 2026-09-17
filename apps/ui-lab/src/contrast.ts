import Color from "colorjs.io";

type Rgb = [number, number, number];

function rgb(value: string) {
  const color = new Color(value).to("srgb").toGamut();
  return {
    channels: color.coords.map((n) => n ?? 0) as Rgb,
    alpha: color.alpha,
  };
}

function composite(value: string, background: Rgb): Rgb {
  const { channels, alpha } = rgb(value);
  return channels.map(
    (channel, i) => channel * alpha + background[i]! * (1 - alpha),
  ) as Rgb;
}

/** Backgrounds run from the opaque outer canvas to the innermost surface. */
export function resolveBackground(backgrounds: string[]): Color {
  if (!backgrounds.length || rgb(backgrounds[0]!).alpha < 1) {
    throw new Error("Contrast requires an opaque canvas");
  }
  const background = backgrounds.reduce<Rgb>(
    (result, layer) => composite(layer, result),
    [0, 0, 0],
  );
  return new Color("srgb", background);
}

export function contrastRatio(
  foreground: string,
  backgrounds: string[],
): number {
  const background = resolveBackground(backgrounds);
  const text = composite(foreground, background.coords as Rgb);
  return Color.contrastWCAG21(new Color("srgb", text), background);
}

export function contrastPasses(ratio: number, minimum: number): boolean {
  return ratio >= minimum;
}
