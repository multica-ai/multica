/**
 * Minimal ArkTS NetInfo implementation for the RNCNetInfo turbo module,
 * vendored from @react-native-oh-tpl/netinfo (trimmed to the surface the
 * JS package consumes: getCurrentState + connectivity-change events). The
 * tpl package cannot be imported directly — its sources reference the
 * `@rnoh/react-native-openharmony/ts` subpath, which does not exist in the
 * installed RNOH har, and its oh-package entry is broken. Registered in
 * MulticaPackage under 'RNCNetInfo'; CPP proxy in cpp/PackageProvider.cpp.
 */
import { UITurboModule } from '@rnoh/react-native-openharmony/ts';
import type { UITurboModuleContext } from '@rnoh/react-native-openharmony/ts';
import connection from '@ohos.net.connection';

export class MulticaNetInfoModule extends UITurboModule {
  private numberOfListeners: number = 0
  private netConnection: connection.NetConnection | null = null

  constructor(protected ctx: UITurboModuleContext) {
    super(ctx);
  }

  getCurrentState(_requestedInterface?: string): Promise<object> {
    return this.readState();
  }

  addListener(_eventName: string): void {
    const wasZero = this.numberOfListeners === 0;
    this.numberOfListeners = this.numberOfListeners + 1
    if (wasZero) {
      this.registerConnection();
    }
    // Match the upstream module contract: a subscriber immediately receives
    // the current state, not just future edges.
    this.readState().then((state) => {
      this.ctx.rnInstance.emitDeviceEvent('netInfo.networkStatusDidChange', state)
    })
  }

  removeListeners(_count: number): void {
    this.numberOfListeners = Math.max(0, this.numberOfListeners - 1)
  }

  private async readState(): Promise<object> {
    const state: Record<string, boolean | string> = {}
    try {
      const netHandle = connection.getDefaultNetSync()
      if (netHandle.netId == 0) {
        state.type = 'none'
        state.isConnected = false
        state.isInternetReachable = false
      } else {
        state.isConnected = true
        const capabilities = await connection.getNetCapabilities(netHandle)
        state.type = this.bearerType(capabilities)
        state.isInternetReachable = capabilities.networkCap.indexOf(16) != -1
      }
    } catch (e) {
      state.type = 'unknown'
      state.isConnected = false
      state.isInternetReachable = false
    }
    return state
  }

  private bearerType(capabilities: connection.NetCapabilities): string {
    const bearers = capabilities.bearerTypes
    if (bearers.length == 1) {
      switch (bearers[0]) {
        case connection.NetBearType.BEARER_CELLULAR:
          return 'cellular'
        case connection.NetBearType.BEARER_WIFI:
          return 'wifi'
        case connection.NetBearType.BEARER_ETHERNET:
          return 'ethernet'
        case connection.NetBearType.BEARER_VPN:
          return 'vpn'
      }
    }
    return 'unknown'
  }

  private registerConnection(): void {
    try {
      this.netConnection = connection.createNetConnection()
      this.netConnection.register((_error) => {})
      const emit = () => {
        if (this.numberOfListeners <= 0 || !this.netConnection) return
        this.readState().then((state) => {
          this.ctx.rnInstance.emitDeviceEvent('netInfo.networkStatusDidChange', state)
        })
      }
      this.netConnection.on('netAvailable', emit)
      this.netConnection.on('netCapabilitiesChange', emit)
      this.netConnection.on('netLost', emit)
      this.netConnection.on('netUnavailable', emit)
    } catch (e) {
      console.error(`[MulticaNetInfo] register failed: ${JSON.stringify(e)}`)
    }
  }
}
