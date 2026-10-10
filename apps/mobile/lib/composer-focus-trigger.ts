/** Whether an expand trigger must wait for the TextInput's first native layout. */
export function shouldWaitForInputLayout(isExpanded: boolean): boolean {
  return !isExpanded;
}

/** Ignore delayed blur cleanup when the reply target changed in the meantime. */
export function shouldCollapseAfterBlur(args: {
  isEmpty: boolean;
  isFocused: boolean;
  triggerRevisionAtBlur: number;
  currentTriggerRevision: number;
}): boolean {
  return (
    args.isEmpty &&
    !args.isFocused &&
    args.triggerRevisionAtBlur === args.currentTriggerRevision
  );
}
