// Clickable controls are rounded rectangles on the radius ladder: rounded-md
// up to 28px tall, rounded-lg for 32–36px. Round is reserved for
// avatars, status dots and counts, switches, radios, sliders, color swatches,
// the floating chat launcher and the composer send/stop button; those carry an
// inline disable naming the exception.
//
// Only an unprefixed `rounded-full` on the className of a button-like element
// is flagged. `after:rounded-full` and friends paint decoration, and a span
// inside a button (a badge, an avatar) is not the control's own shape.
//
// Exported for `no-restricted-syntax`, whose options flat config replaces
// wholesale: a package that sets its own entries must list this one again.

const CONTROL =
  "JSXOpeningElement[name.name=/^(button|a|Button|IconButton|AppLink|PillButton|ClearablePillButton)$/]";
const CLASS_NAME = `${CONTROL} > JSXAttribute[name.name='className']`;
const ROUND = "/(^|\\s)rounded-full(\\s|$)/";

export const NO_ROUND_CONTROLS = {
  selector: [
    `${CLASS_NAME} Literal[value=${ROUND}]`,
    `${CLASS_NAME} TemplateElement[value.raw=${ROUND}]`,
  ].join(", "),
  message:
    "Buttons, links and chips are rounded rectangles (rounded-md up to 28px, rounded-lg for 32–36px). Keep rounded-full for the named exceptions — avatars, dots and counts, switches, radios, swatches, the chat launcher, the composer send button — and mark one with an inline disable that names it.",
};
