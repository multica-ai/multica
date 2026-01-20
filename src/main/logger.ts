import log from 'electron-log/main'
import { is } from '@electron-toolkit/utils'

// Initialize electron-log
log.initialize()

// Configure log levels
// Production: write to file (info and above)
// Development: console only, no file
log.transports.file.level = is.dev ? false : 'info'
log.transports.console.level = 'debug'

// Log file location (production): ~/Library/Logs/Multica/main.log (macOS)

// Catch unhandled errors
log.errorHandler.startCatching()

export default log
