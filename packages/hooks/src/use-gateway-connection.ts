"use client";

import { useState, useEffect, useCallback, useRef } from "react";
import { v7 as uuidv7 } from "uuid";
import {
  GatewayClient,
  type ConnectionState,
} from "@multica/sdk";

// Persisted connection identity (separate from one-time token)
const STORAGE_KEY = "multica-connection-identity";
const DEVICE_KEY = "multica-device-id";

export interface ConnectionIdentity {
  gateway: string;
  hubId: string;
  agentId: string;
}

function loadIdentity(): ConnectionIdentity | null {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return null;
    const parsed = JSON.parse(raw);
    if (parsed.gateway && parsed.hubId && parsed.agentId) return parsed;
    return null;
  } catch {
    return null;
  }
}

function saveIdentity(identity: ConnectionIdentity): void {
  localStorage.setItem(STORAGE_KEY, JSON.stringify(identity));
}

function clearIdentity(): void {
  localStorage.removeItem(STORAGE_KEY);
}

function getDeviceId(): string {
  let id = localStorage.getItem(DEVICE_KEY);
  if (!id) {
    id = uuidv7();
    localStorage.setItem(DEVICE_KEY, id);
  }
  return id;
}

export type PageState = "loading" | "not-connected" | "connecting" | "connected";

export interface UseGatewayConnectionReturn {
  pageState: PageState;
  /** Raw SDK connection state — used by ConnectAgent for verifying/connecting distinction */
  connectionState: ConnectionState;
  identity: ConnectionIdentity | null;
  error: string | null;
  client: GatewayClient | null;
  /** Increments on each disconnect — use as React key to reset child components */
  pairingKey: number;
  connect: (identity: ConnectionIdentity, token?: string) => void;
  disconnect: () => void;
}

export function useGatewayConnection(): UseGatewayConnectionReturn {
  const [pageState, setPageState] = useState<PageState>("loading");
  const [connectionState, setConnectionState] = useState<ConnectionState>("disconnected");
  const [identity, setIdentity] = useState<ConnectionIdentity | null>(null);
  const [error, setError] = useState<string | null>(null);
  const clientRef = useRef<GatewayClient | null>(null);
  const disconnectingRef = useRef(false);
  const pairingKeyRef = useRef(0);

  const connectToGateway = useCallback(
    (id: ConnectionIdentity, token?: string) => {
      const doConnect = () => {
        disconnectingRef.current = false;
        setPageState("connecting");
        setError(null);

        const deviceId = getDeviceId();

        const client = new GatewayClient({
          url: id.gateway,
          deviceId,
          deviceType: "client",
          hubId: id.hubId,
          ...(token ? { token } : {}),
        })
          .onStateChange((state: ConnectionState) => {
            console.log("[GatewayConnection] state:", state);
            if (disconnectingRef.current) return;
            setConnectionState(state);
            if (state === "registered") {
              saveIdentity(id);
              setIdentity(id);
              setPageState("connected");
            }
          })
          .onError((err: Error) => {
            console.log("[GatewayConnection] error:", err.message);
            if (disconnectingRef.current) return;
            pairingKeyRef.current += 1;
            clearIdentity();
            setIdentity(null);
            setError(err.message);
            setPageState("not-connected");
            clientRef.current?.disconnect();
            clientRef.current = null;
          })
          .onSendError((err) => {
            if (disconnectingRef.current) return;
            setError(err.error);
          });

        clientRef.current = client;
        client.connect();
      };

      // If there's an existing client, disconnect first and wait for Gateway to process
      if (clientRef.current) {
        clientRef.current.disconnect();
        clientRef.current = null;
        setTimeout(doConnect, 300);
      } else {
        doConnect();
      }
    },
    [],
  );

  // Try to reconnect with saved identity on mount
  useEffect(() => {
    const saved = loadIdentity();
    console.log("[GatewayConnection] mount, saved identity:", saved);
    if (!saved) {
      setPageState("not-connected");
      return;
    }

    setIdentity(saved);
    // Delay reconnection — if a previous socket just disconnected (e.g. StrictMode
    // cleanup or page navigation), the Gateway needs time to process it
    const timer = setTimeout(() => connectToGateway(saved), 300);

    return () => {
      clearTimeout(timer);
      clientRef.current?.disconnect();
      clientRef.current = null;
    };
  }, []);

  const disconnect = useCallback(() => {
    disconnectingRef.current = true;
    pairingKeyRef.current += 1;
    clientRef.current?.disconnect();
    clientRef.current = null;
    clearIdentity();
    setIdentity(null);
    setPageState("not-connected");
    setConnectionState("disconnected");
    setError(null);
  }, []);

  return {
    pageState,
    connectionState,
    identity,
    error,
    client: clientRef.current,
    pairingKey: pairingKeyRef.current,
    connect: connectToGateway,
    disconnect,
  };
}
