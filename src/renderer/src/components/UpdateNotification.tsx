/**
 * Update notification component
 * Shows when a new version is available and allows user to download/install
 */
import { useState, useEffect } from 'react'
import { Download, RefreshCw, X, CheckCircle, AlertCircle } from 'lucide-react'
import { Button } from '@/components/ui/button'
import type { UpdateStatus } from '../../../shared/electron-api'

export function UpdateNotification(): React.JSX.Element | null {
  const [updateStatus, setUpdateStatus] = useState<UpdateStatus | null>(null)
  const [dismissed, setDismissed] = useState(false)

  useEffect(() => {
    const unsubscribe = window.electronAPI.onUpdateStatus((status) => {
      setUpdateStatus(status)
      // Reset dismissed state when a new update becomes available
      if (status.status === 'available') {
        setDismissed(false)
      }
    })

    return () => unsubscribe()
  }, [])

  const handleDownload = async () => {
    await window.electronAPI.downloadUpdate()
  }

  const handleInstall = () => {
    window.electronAPI.installUpdate()
  }

  const handleDismiss = () => {
    setDismissed(true)
  }

  // Don't show if dismissed or no relevant status
  if (dismissed) return null
  if (!updateStatus) return null
  if (updateStatus.status === 'checking' || updateStatus.status === 'not-available') return null

  const version = updateStatus.info?.version
  const isError = updateStatus.status === 'error'

  return (
    <div className="fixed bottom-4 right-4 z-50 animate-in slide-in-from-bottom-2 fade-in duration-300">
      <div className="flex items-center gap-3 rounded-lg border bg-card p-3 shadow-lg">
        {/* Icon */}
        <div className={`flex h-8 w-8 items-center justify-center rounded-full ${isError ? 'bg-destructive/10' : 'bg-primary/10'}`}>
          {isError ? (
            <AlertCircle className="h-4 w-4 text-destructive" />
          ) : updateStatus.status === 'downloaded' ? (
            <CheckCircle className="h-4 w-4 text-primary" />
          ) : updateStatus.status === 'downloading' ? (
            <RefreshCw className="h-4 w-4 text-primary animate-spin" />
          ) : (
            <Download className="h-4 w-4 text-primary" />
          )}
        </div>

        {/* Content */}
        <div className="flex flex-col gap-0.5">
          <span className="text-sm font-medium">
            {isError
              ? 'Update failed'
              : updateStatus.status === 'downloaded'
                ? 'Update ready'
                : updateStatus.status === 'downloading'
                  ? 'Downloading update...'
                  : 'Update available'}
          </span>
          <span className="text-xs text-muted-foreground">
            {isError
              ? 'Please download manually from GitHub'
              : updateStatus.status === 'downloading' && updateStatus.progress
                ? `${Math.round(updateStatus.progress.percent)}%`
                : version
                  ? `Version ${version}`
                  : 'New version available'}
          </span>
        </div>

        {/* Actions */}
        <div className="flex items-center gap-1 ml-2">
          {updateStatus.status === 'available' && (
            <Button size="sm" variant="default" onClick={handleDownload}>
              Download
            </Button>
          )}
          {updateStatus.status === 'downloaded' && (
            <Button size="sm" variant="default" onClick={handleInstall}>
              Restart
            </Button>
          )}
          {isError && (
            <Button
              size="sm"
              variant="outline"
              onClick={() => window.open('https://github.com/multica-ai/multica/releases', '_blank')}
            >
              View Releases
            </Button>
          )}
          {updateStatus.status !== 'downloading' && (
            <Button size="icon" variant="ghost" className="h-7 w-7" onClick={handleDismiss}>
              <X className="h-4 w-4" />
            </Button>
          )}
        </div>
      </div>
    </div>
  )
}
