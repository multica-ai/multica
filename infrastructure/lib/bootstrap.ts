import { readFileSync } from 'node:fs';
import { join } from 'node:path';

/**
 * Reads a script from assets/ and substitutes its @@-marked placeholders.
 *
 * The scripts live in their own files rather than in template literals so they
 * can be read, reviewed and syntax-checked as shell (see the workflow's
 * `bash -n` step). Values may be CDK tokens: the rendered string goes through
 * Fn::Base64, so a token resolves to a CloudFormation intrinsic and the
 * instance receives the real value.
 */
export function renderScript(fileName: string, substitutions: Record<string, string>): string {
  const source = readFileSync(join(__dirname, '..', 'assets', fileName), 'utf8');
  const rendered = source.replace(/@@([A-Z_]+)@@/g, (_match, name: string) => {
    const value = substitutions[name];
    if (value === undefined) {
      throw new Error(`assets/${fileName} references unknown placeholder @@${name}@@`);
    }
    return value;
  });

  // A leftover marker means the asset and its substitution map have drifted
  // apart, which would ship a machine that configures itself with the literal
  // text.
  const leftover = rendered.match(/@@\w+@@/);
  if (leftover) {
    throw new Error(`assets/${fileName} still contains ${leftover[0]} after rendering`);
  }
  return rendered;
}
