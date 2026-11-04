import { action, makeObservable, observable } from "mobx";
import "../mobx-config";

export type ActivationStatus = "requested" | "uncertain" | "confirmed" | "failed";
export type ActivationSettingsStatus = "inFlight" | "failed";

export interface ActivationRequest<Settings> {
  mode: "new" | "existing";
  requestId: string;
  agentSessionId: string;
  settings?: Settings;
}

/** One attempt to start or attach to a conversation, from asking until it is known to exist or has failed. */
export class ActivationModel<Request extends ActivationRequest<Settings>, Settings> {
  status: ActivationStatus;
  errorCode: string | null = null;
  errorMessage: string | null = null;
  settings: Settings | undefined;
  settingsPatch: Settings | undefined = undefined;
  settingsStatus: ActivationSettingsStatus | undefined = undefined;

  constructor(
    readonly request: Request,
    readonly requestedAtUnixMs: number,
    readonly expiresAtUnixMs: number,
    status: ActivationStatus = "requested",
  ) {
    this.status = status;
    this.settings = request.settings;
    makeObservable(this, {
      status: observable,
      errorCode: observable,
      errorMessage: observable,
      settings: observable.ref,
      settingsPatch: observable.ref,
      settingsStatus: observable,
      confirm: action,
      markUncertain: action,
      fail: action,
      patchSettings: action,
      beginSettings: action,
      settingsApplied: action,
      settingsFailed: action,
    });
  }

  get pending(): boolean { return this.status === "requested" || this.status === "uncertain"; }

  confirm(): void {
    this.status = "confirmed";
    this.errorMessage = null;
  }

  markUncertain(): void { this.status = "uncertain"; }

  fail(code: string | null, message: string | null): void {
    this.status = "failed";
    this.errorCode = code;
    this.errorMessage = message;
  }

  patchSettings(patch: Settings): void {
    this.settingsPatch = { ...this.settingsPatch, ...patch };
    this.settings = { ...this.settings, ...patch } as Settings;
    this.settingsStatus = undefined;
  }

  beginSettings(): void { this.settingsStatus = "inFlight"; }

  settingsApplied(): void {
    this.settingsPatch = undefined;
    this.settingsStatus = undefined;
  }

  settingsFailed(code: string | null, message: string | null): void {
    this.errorCode = code;
    this.errorMessage = message;
    this.settingsStatus = "failed";
  }
}
