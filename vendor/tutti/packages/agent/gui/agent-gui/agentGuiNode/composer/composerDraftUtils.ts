
export const AGENT_COMPOSER_PASTED_TEXT_FILE_PREFIX = "pasted-text";

export function agentComposerTextByteLength(text: string): number {
  if (typeof TextEncoder !== "undefined") {
    return new TextEncoder().encode(text).byteLength;
  }
  return text.length;
}

