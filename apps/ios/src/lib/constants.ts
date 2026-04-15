/**
 * API configuration.
 *
 * For local dev, the iOS simulator can reach the host machine at localhost.
 * For physical device testing, replace with your machine's LAN IP.
 */
export const API_BASE_URL =
  process.env.EXPO_PUBLIC_API_URL ?? "http://localhost:8080";

export const WS_URL =
  process.env.EXPO_PUBLIC_WS_URL ?? "ws://localhost:8080/ws";

export const APP_URL = process.env.EXPO_PUBLIC_APP_URL;
