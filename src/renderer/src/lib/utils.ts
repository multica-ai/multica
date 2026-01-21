import { clsx, type ClassValue } from 'clsx'
import { twMerge } from 'tailwind-merge'

export function cn(...inputs: ClassValue[]): string {
  return twMerge(clsx(inputs))
}

/**
 * Format duration in human-readable form
 * @param ms Duration in milliseconds
 * @returns "5.23s" for under a minute (with 2 decimal places), "1m 29s" for 1+ minutes
 */
export function formatDuration(ms: number): string {
  const totalSeconds = ms / 1000
  if (totalSeconds < 60) {
    return `${totalSeconds.toFixed(2)}s`
  }
  const minutes = Math.floor(totalSeconds / 60)
  const remainingSeconds = Math.floor(totalSeconds % 60)
  if (remainingSeconds === 0) return `${minutes}m`
  return `${minutes}m ${remainingSeconds}s`
}

/**
 * Format timestamp for tooltip display
 * @param timestamp ISO 8601 string or timestamp number
 * @returns English datetime string (e.g., "January 21, 2026, 3:03 PM") or "Unknown time" for invalid dates
 */
export function formatLocalizedDatetime(timestamp: string | number): string {
  const date = typeof timestamp === 'number' ? new Date(timestamp) : new Date(timestamp)
  if (isNaN(date.getTime())) {
    return 'Unknown time'
  }
  return date.toLocaleString('en-US', {
    year: 'numeric',
    month: 'long',
    day: 'numeric',
    hour: 'numeric',
    minute: '2-digit',
    hour12: true
  })
}
