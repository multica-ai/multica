// Google OAuth `state` is a comma-separated list of `key:value` fields.
// `next` is a path the login page chose, and that path may itself contain
// commas (a multi-value query). Leaving it raw splits the field: the
// destination is truncated, and the tail can be read as another field —
// including `cli_callback`, which the callback will redirect a JWT to.
// Encoding the value keeps the comma inside this field. Decoding is
// identity for the older unencoded paths that contain no `%` sequences
// (`/invite/abc`), so a login started before this change still resolves.

export type GoogleOAuthStateInput = {
  platform: string | null;
  nextUrl: string | null;
  cliCallback: string | null;
  cliState: string;
};

export type GoogleOAuthState = {
  isDesktop: boolean;
  nextRaw: string | null;
  cliCallbackRaw: string | null;
  cliState: string;
};

export function buildGoogleOAuthState(
  input: GoogleOAuthStateInput,
): string | undefined {
  const parts = [
    input.platform === "desktop" ? "platform:desktop" : "",
    input.nextUrl ? `next:${encodeURIComponent(input.nextUrl)}` : "",
    input.cliCallback
      ? `cli_callback:${encodeURIComponent(input.cliCallback)}`
      : "",
    input.cliState ? `cli_state:${encodeURIComponent(input.cliState)}` : "",
  ].filter(Boolean);
  return parts.join(",") || undefined;
}

export function parseGoogleOAuthState(state: string): GoogleOAuthState {
  const stateParts = state.split(",");
  const nextPart = stateParts.find((part) => part.startsWith("next:"));
  const cliCallbackPart = stateParts.find((part) =>
    part.startsWith("cli_callback:"),
  );
  const cliStatePart = stateParts.find((part) => part.startsWith("cli_state:"));
  return {
    isDesktop: stateParts.includes("platform:desktop"),
    nextRaw: nextPart
      ? decodeStateValue(nextPart.slice("next:".length))
      : null,
    cliCallbackRaw: cliCallbackPart
      ? decodeStateValue(cliCallbackPart.slice("cli_callback:".length))
      : null,
    cliState: cliStatePart
      ? decodeStateValue(cliStatePart.slice("cli_state:".length))
      : "",
  };
}

function decodeStateValue(value: string): string {
  try {
    return decodeURIComponent(value);
  } catch {
    // A stray `%` from an older unencoded path is not a valid escape.
    return value;
  }
}
