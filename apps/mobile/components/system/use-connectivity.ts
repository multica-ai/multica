import { useEffect, useState } from "react";
import NetInfo from "@react-native-community/netinfo";

import type { ConnectivitySnapshot } from "@/data/network-transitions";

const UNKNOWN: ConnectivitySnapshot = {
  isConnected: null,
  isInternetReachable: null,
};

/**
 * NetInfo snapshot for UI that must not render a speculative offline state.
 * `null` means "not known yet" and callers treat it as online.
 */
export function useConnectivity(): ConnectivitySnapshot {
  const [snapshot, setSnapshot] = useState<ConnectivitySnapshot>(UNKNOWN);

  useEffect(() => {
    const unsubscribe = NetInfo.addEventListener((state) => {
      setSnapshot({
        isConnected: state.isConnected,
        isInternetReachable: state.isInternetReachable,
      });
    });

    void NetInfo.fetch()
      .then((state) => {
        setSnapshot({
          isConnected: state.isConnected,
          isInternetReachable: state.isInternetReachable,
        });
      })
      .catch(() => {
        // A failed probe leaves the unknown state; the banner stays hidden and
        // the next listener event resolves it.
      });

    return unsubscribe;
  }, []);

  return snapshot;
}
