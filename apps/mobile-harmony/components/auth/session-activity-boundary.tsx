/**
 * Notices that someone is actually using the app, so the sliding session can
 * follow use rather than a clock (MUL-7436). Verbatim port of
 * apps/mobile/components/auth/session-activity-boundary — the responder
 * config lives in data/session-activity (ported) and declines the gesture it
 * observes, so touches behave exactly as they would without this wrapper.
 */
import type { ReactNode } from "react";
import { useMemo } from "react";
import { PanResponder, View } from "react-native";

import { sessionActivityResponderConfig } from "@/data/session-activity";

export function SessionActivityBoundary({ children }: { children: ReactNode }) {
  const responder = useMemo(
    () => PanResponder.create(sessionActivityResponderConfig),
    [],
  );

  return (
    <View style={{ flex: 1 }} {...responder.panHandlers}>
      {children}
    </View>
  );
}
