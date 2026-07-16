export interface LifecycleResult {
  success: boolean;
  error?: string;
}

export function daemonStopArgs(profile: string, requireIdle: boolean): string[] {
  return [
    "daemon",
    "stop",
    ...(requireIdle ? ["--require-idle"] : []),
    ...(profile ? ["--profile", profile] : []),
  ];
}

export class DaemonLifecycleCoordinator {
  private inProgress = false;

  async run<T extends LifecycleResult>(operation: () => Promise<T>): Promise<T | LifecycleResult> {
    if (this.inProgress) {
      return { success: false, error: "Another daemon operation is in progress" };
    }
    this.inProgress = true;
    try {
      return await operation();
    } finally {
      this.inProgress = false;
    }
  }
}
