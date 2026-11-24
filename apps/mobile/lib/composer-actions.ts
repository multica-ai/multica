export interface ComposerActionInput {
  /** A run is active for this conversation. */
  isSending: boolean;
  /** The owner accepts sends during a run (chat on a queue-capable server). */
  allowSubmitWhileSending: boolean;
  /** The owner provided a Stop control. */
  hasStop: boolean;
  /** The owner provided a queue-for-later action. */
  hasQueueAction: boolean;
  hasContent: boolean;
  hasInFlightUpload: boolean;
  submitting: boolean;
  disabled: boolean;
}

export interface ComposerActions {
  canSend: boolean;
  showStop: boolean;
  showQueue: boolean;
}

/**
 * Which trailing actions the composer offers. One slot holds Send or Stop:
 * during a run that accepts sends, content claims the slot for Send and an
 * empty composer (or an upload) keeps Stop reachable, matching web chat.
 * While a send is in flight the slot stays on a disabled Send: the composer
 * clears optimistically, and swapping straight to Stop would put a
 * run-ending button under the finger that just tapped Send.
 */
export function composerActions(input: ComposerActionInput): ComposerActions {
  const submitWhileSending = input.isSending && input.allowSubmitWhileSending;
  const canSend =
    !input.disabled &&
    (!input.isSending || input.allowSubmitWhileSending) &&
    !input.submitting &&
    !input.hasInFlightUpload &&
    input.hasContent;
  const showStop =
    input.isSending &&
    input.hasStop &&
    (!submitWhileSending ||
      input.hasInFlightUpload ||
      (!input.hasContent && !input.submitting));
  const showQueue =
    submitWhileSending &&
    input.hasQueueAction &&
    input.hasContent &&
    !input.hasInFlightUpload;
  return { canSend, showStop, showQueue };
}
