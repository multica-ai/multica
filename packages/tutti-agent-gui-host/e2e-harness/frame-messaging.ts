type FrameLocation = Pick<Location, "origin" | "protocol">;

/** File pages serialize their origin differently in Location and MessageEvent. */
export function isAgentGuiFrameMessage(
  event: Pick<MessageEvent, "source" | "origin">,
  expectedSource: Window | null,
  location: FrameLocation = window.location,
): boolean {
  if (!expectedSource || event.source !== expectedSource) return false;
  if (location.protocol === "file:") return event.origin === "null" || event.origin === "file://";
  return location.origin !== "null" && event.origin === location.origin;
}

export function agentGuiMessageTargetOrigin(location: FrameLocation = window.location): string {
  return location.protocol === "file:" || location.origin === "null" ? "*" : location.origin;
}
