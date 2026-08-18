import type { LocaleResources } from '@multica/core/i18n';

const TASK_TERMS: ReadonlyArray<readonly [RegExp, string]> = [
  [/\bAn Issue\b/gu, 'A Task'],
  [/\ban issue\b/gu, 'a task'],
  [/\bIssues\b/gu, 'Tasks'],
  [/\bIssue\b/gu, 'Task'],
  [/\bissues\b/gu, 'tasks'],
  [/\bissue\b/gu, 'task'],
];

function adaptTaskTerms(value: unknown): unknown {
  if (typeof value === 'string') {
    return TASK_TERMS.reduce(
      (copy, [pattern, replacement]) => copy.replace(pattern, replacement),
      value
    );
  }
  if (Array.isArray(value)) return value.map(adaptTaskTerms);
  if (value !== null && typeof value === 'object') {
    return Object.fromEntries(
      Object.entries(value).map(([key, child]) => [key, adaptTaskTerms(child)])
    );
  }
  return value;
}

/** Keep Multica's issue domain intact while VIBES presents it as Task. */
export function createTagTaskResources(
  resources: LocaleResources
): LocaleResources {
  return adaptTaskTerms(resources) as LocaleResources;
}
