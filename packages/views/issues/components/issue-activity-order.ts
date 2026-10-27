import type { IssueActivityOrder } from "@multica/core/issues/stores";

/**
 * Reorders only the values occupying activity slots. Every comment and run
 * stays at its existing index, so changing this preference cannot reorder a
 * discussion or move an in-flight run.
 */
export function orderActivitySlots<T>(
  entries: readonly T[],
  order: IssueActivityOrder,
  isActivity: (entry: T) => boolean,
): T[] {
  if (order === "oldest") return [...entries];

  const activities = entries.filter(isActivity).toReversed();
  let activityIndex = 0;
  return entries.map((entry) =>
    isActivity(entry) ? activities[activityIndex++]! : entry,
  );
}
