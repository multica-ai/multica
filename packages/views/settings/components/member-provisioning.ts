export interface ParsedProvisioningEmails {
  emails: string[];
  duplicates: string[];
  invalid: string[];
  total: number;
}

const SIMPLE_EMAIL_PATTERN = /^[^\s@]+@[^\s@]+$/;

export function parseProvisioningEmails(input: string): ParsedProvisioningEmails {
  const tokens = input
    .split(/[\s,;]+/)
    .map((token) => token.trim().toLowerCase())
    .filter(Boolean);
  const emails: string[] = [];
  const duplicates: string[] = [];
  const invalid: string[] = [];
  const seen = new Set<string>();

  for (const token of tokens) {
    if (!SIMPLE_EMAIL_PATTERN.test(token) || token.length > 254) {
      invalid.push(token);
      continue;
    }
    if (seen.has(token)) {
      duplicates.push(token);
      continue;
    }
    seen.add(token);
    emails.push(token);
  }

  return { emails, duplicates, invalid, total: tokens.length };
}
