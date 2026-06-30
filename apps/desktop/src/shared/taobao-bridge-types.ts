export type TaobaoBridgeStatusState =
  | "not_configured"
  | "python_missing"
  | "installing"
  | "stopped"
  | "starting"
  | "running"
  | "unhealthy"
  | "error";

export interface TaobaoBridgeStatus {
  state: TaobaoBridgeStatusState;
  pid?: number;
  port?: number;
  baseUrl?: string;
  runtimeDir?: string;
  logPath?: string;
  message?: string;
  startedAt?: string;
}

export interface TaobaoBridgePublicConfig {
  configured: boolean;
  port: number;
  baseUrl: string;
  env: string;
  runtimeDir: string;
  logPath: string;
  hasEventSecret: boolean;
  hasBridgeToken: boolean;
  hasOrderApiToken: boolean;
  webhookConfigured: boolean;
  orderApiBaseUrl: string;
  orderApiGetOrderPath: string;
  orderApiAuthHeader: string;
  orderApiAuthScheme: string;
  orderApiTimeoutSeconds: number;
  orderApiWriteThrough: boolean;
  allowPlainReceiverInfo: boolean;
  remoteAreaKeywords: string;
  unsupportedAreaKeywords: string;
}

export interface TaobaoBridgeConfigInput {
  port?: number;
  multicaAutopilotWebhookUrl?: string;
  orderApiBaseUrl?: string;
  orderApiToken?: string;
  orderApiGetOrderPath?: string;
  orderApiAuthHeader?: string;
  orderApiAuthScheme?: string;
  orderApiTimeoutSeconds?: number;
  orderApiWriteThrough?: boolean;
  allowPlainReceiverInfo?: boolean;
  remoteAreaKeywords?: string;
  unsupportedAreaKeywords?: string;
}

export interface TaobaoWorkflowAssetInstallResult {
  runtimeDir: string;
  skillDir: string;
  skillKey: string;
}

export interface TaobaoBridgeAgentEnvironment {
  ORDER_BRIDGE_BASE_URL: string;
  ORDER_BRIDGE_API_TOKEN: string;
}
