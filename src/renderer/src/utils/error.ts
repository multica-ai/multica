/**
 * Utility function to extract error messages from various error types
 * Handles Electron IPC serialized errors where Error.message is lost
 */
export function getErrorMessage(err: unknown): string {
  if (err instanceof Error) {
    return err.message
  }
  if (typeof err === 'string') {
    return err
  }
  if (err && typeof err === 'object') {
    // Handle Electron IPC serialized errors with message property
    if ('message' in err && typeof err.message === 'string') {
      return err.message
    }
    // Try to stringify for debugging
    try {
      return JSON.stringify(err)
    } catch {
      return 'Unknown error'
    }
  }
  return 'Unknown error'
}
