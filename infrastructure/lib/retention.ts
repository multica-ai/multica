import * as logs from 'aws-cdk-lib/aws-logs';

/**
 * Maps a day count to the RetentionDays enum CloudWatch accepts.
 *
 * Retention is not a free-form number. Silently rounding a rejected value to
 * the nearest accepted one would hide the mistake until the API call failed
 * mid-deploy, so an unusable value fails at synth and names the alternatives.
 */
export function resolveRetention(days: number): logs.RetentionDays {
  const match = Object.entries(logs.RetentionDays).find(([, value]) => value === days);
  if (!match) {
    const allowed = Object.values(logs.RetentionDays)
      .filter((value): value is number => typeof value === 'number')
      .sort((a, b) => a - b)
      .join(', ');
    throw new Error(`logRetentionDays ${days} is not a CloudWatch retention period; allowed: ${allowed}`);
  }
  return match[1] as logs.RetentionDays;
}
