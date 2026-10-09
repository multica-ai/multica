// Direct-message access control. The plugin supplies the allow list (its
// pairing store, falling back to the Weixin user who scanned the login QR);
// a sender outside it is dropped. Unlike OpenClaw there is no pairing-code
// flow for unknown senders.
export async function resolveSenderCommandAuthorizationWithRuntime({
  senderId,
  isSenderAllowed,
  readAllowFromStore,
}) {
  const allowFrom = (await readAllowFromStore?.()) ?? [];
  // Fail closed: an empty list means nobody has been authorized yet.
  const allowed = allowFrom.length > 0 && isSenderAllowed(senderId, allowFrom);
  return { senderAllowedForCommands: allowed, commandAuthorized: allowed };
}

export function resolveDirectDmAuthorizationOutcome({ senderAllowedForCommands }) {
  return senderAllowedForCommands ? "allowed" : "unauthorized";
}
