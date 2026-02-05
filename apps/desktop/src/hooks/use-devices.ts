import { useState, useEffect, useCallback } from 'react'

export interface DeviceMeta {
  userAgent?: string
  platform?: string
  language?: string
}

export interface DeviceEntry {
  deviceId: string
  agentId: string
  addedAt: number
  meta?: DeviceMeta
}

export interface UseDevicesReturn {
  devices: DeviceEntry[]
  loading: boolean
  refresh: () => Promise<void>
  revokeDevice: (deviceId: string) => Promise<boolean>
}

export function useDevices(): UseDevicesReturn {
  const [devices, setDevices] = useState<DeviceEntry[]>([])
  const [loading, setLoading] = useState(true)

  const refresh = useCallback(async () => {
    try {
      const list = await window.electronAPI?.hub.listDevices()
      setDevices((list as DeviceEntry[]) ?? [])
    } catch (err) {
      console.error('Failed to load devices:', err)
    } finally {
      setLoading(false)
    }
  }, [])

  const revokeDevice = useCallback(async (deviceId: string): Promise<boolean> => {
    try {
      const result = await window.electronAPI?.hub.revokeDevice(deviceId)
      if (result?.ok) {
        setDevices((prev) => prev.filter((d) => d.deviceId !== deviceId))
        return true
      }
      return false
    } catch (err) {
      console.error('Failed to revoke device:', err)
      return false
    }
  }, [])

  useEffect(() => {
    refresh()
  }, [refresh])

  // Subscribe to device list changes pushed from main process
  useEffect(() => {
    window.electronAPI?.hub.onDevicesChanged(() => {
      refresh()
    })
    return () => {
      window.electronAPI?.hub.offDevicesChanged()
    }
  }, [refresh])

  return { devices, loading, refresh, revokeDevice }
}
