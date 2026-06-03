export type VersionInfo = {
  commit: string;
  builtAt: string | null;
};

type VersionEnv = {
  COMMIT_SHA?: string | null;
  BUILD_TIME?: string | null;
};

export function readVersionInfo(env: VersionEnv): VersionInfo {
  const commit = env.COMMIT_SHA?.trim() ?? "";
  const builtAt = env.BUILD_TIME?.trim() ?? "";
  return {
    commit: commit.length > 0 ? commit : "unknown",
    builtAt: builtAt.length > 0 ? builtAt : null,
  };
}
