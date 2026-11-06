declare module "#verification-dev/main" {
  export const verificationDev:
    | {
        readonly enabled: boolean;
        readonly runtimeHome: string | undefined;
        configureBeforeReady(): void;
        start(options: {
          windows(): import("electron").BrowserWindow[];
          currentUser(): string | null;
          apiOrigin(): string;
          endpoint(): Promise<{ base: string; token: string }>;
          identity(): Promise<{
            ownerId: string;
            runId: string;
            pid: number;
            cliCommit: string;
          }>;
          backendToken(): Promise<string | null>;
          deliverToken(token: string): void;
        }): Promise<void>;
      }
    | undefined;
}
declare module "#verification-dev/preload" {
  export const verificationPreload: { install(): void } | undefined;
}
declare module "#verification-dev/renderer" {
  export const verificationRenderer:
    | {
        registerService(
          service: import("@multica/conversation-client/services").ConversationService,
        ): void;
      }
    | undefined;
}
