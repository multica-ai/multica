export type MobilePushPlatform = "android" | "ios";
export type MobilePushProvider = "getui" | "apns";

export interface UpsertMobilePushRegistrationRequest {
  installation_id: string;
  platform: MobilePushPlatform;
  provider: MobilePushProvider;
  provider_client_id: string;
  app_version?: string;
}

export interface MobilePushRegistrationResponse {
  id: string;
  user_id: string;
  installation_id: string;
  platform: MobilePushPlatform;
  provider: MobilePushProvider;
  provider_client_id: string;
  app_version: string | null;
  enabled: boolean;
  last_seen_at: string;
  created_at: string;
  updated_at: string;
}
