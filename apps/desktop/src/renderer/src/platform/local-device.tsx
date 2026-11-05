import { createContext, useContext, type ReactNode } from "react";
import {
  localV2Transport,
  type DirectLocalV2Transport
} from "./local-v2-transport";
import { observer } from "mobx-react-lite";
export const LocalDeviceProvider = observer(LocalDeviceProviderRender);

/**
 * The device whose conversations a page shows: this one, or another device
 * of the account reached over Device link.
 */
export interface LocalDevice {
  /** "local", or the other device's peer ID. */
  id: string;
  /** The other device's name; empty for this one. */
  name: string;
  transport: DirectLocalV2Transport;
}

const LocalDeviceContext = createContext<LocalDevice | null>(null);

function LocalDeviceProviderRender({
  device,
  children
}: {
  device: LocalDevice;
  children: ReactNode;
}) {
  return (
    <LocalDeviceContext.Provider value={device}>
      {children}
    </LocalDeviceContext.Provider>
  );
}

let thisDevice: LocalDevice | undefined;

/** The page's device; outside a Device link page, this one. */
export function useLocalDevice(): LocalDevice {
  const device = useContext(LocalDeviceContext);
  if (device) return device;
  const transport = localV2Transport();
  if (thisDevice?.transport !== transport)
    thisDevice = { id: "local", name: "", transport };
  return thisDevice;
}
