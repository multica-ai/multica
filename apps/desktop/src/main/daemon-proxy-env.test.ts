import { describe, expect, it, vi } from "vitest";

import { desktopSpawnEnv } from "./daemon-proxy-env";

const macProxyOutput = `
<dictionary> {
  HTTPEnable : 1
  HTTPPort : 7897
  HTTPProxy : 127.0.0.1
  HTTPSEnable : 1
  HTTPSPort : 7898
  HTTPSProxy : proxy.local
  ExceptionsList : <array> {
    0 : 127.0.0.1
    1 : *.local
  }
  ExcludeSimpleHostnames : 1
}
`;

describe("desktopSpawnEnv", () => {
  it("marks Desktop-launched daemon processes", () => {
    expect(desktopSpawnEnv({}, "linux").MULTICA_LAUNCHED_BY).toBe("desktop");
  });

  it("fills missing macOS proxy env from system settings", () => {
    const env = desktopSpawnEnv({ PATH: "/usr/bin" }, "darwin", () => macProxyOutput);

    expect(env.HTTP_PROXY).toBe("http://127.0.0.1:7897");
    expect(env.http_proxy).toBe("http://127.0.0.1:7897");
    expect(env.HTTPS_PROXY).toBe("http://proxy.local:7898");
    expect(env.https_proxy).toBe("http://proxy.local:7898");
    expect(env.NO_PROXY).toBe("127.0.0.1,*.local,localhost");
    expect(env.no_proxy).toBe("127.0.0.1,*.local,localhost");
  });

  it("does not override explicit proxy env", () => {
    const env = desktopSpawnEnv(
      {
        HTTP_PROXY: "http://shell-proxy:8888",
        HTTPS_PROXY: "http://shell-secure-proxy:8889",
        NO_PROXY: "localhost,internal",
      },
      "darwin",
      () => macProxyOutput,
    );

    expect(env.HTTP_PROXY).toBe("http://shell-proxy:8888");
    expect(env.http_proxy).toBe("http://shell-proxy:8888");
    expect(env.HTTPS_PROXY).toBe("http://shell-secure-proxy:8889");
    expect(env.https_proxy).toBe("http://shell-secure-proxy:8889");
    expect(env.NO_PROXY).toBe("localhost,internal");
    expect(env.no_proxy).toBe("localhost,internal");
  });

  it("does not read macOS proxy settings on other platforms", () => {
    const readProxy = vi.fn(() => macProxyOutput);
    const env = desktopSpawnEnv({}, "linux", readProxy);

    expect(readProxy).not.toHaveBeenCalled();
    expect(env.HTTP_PROXY).toBeUndefined();
    expect(env.HTTPS_PROXY).toBeUndefined();
  });

  it("leaves proxy env unset when macOS proxy settings are disabled", () => {
    const env = desktopSpawnEnv(
      {},
      "darwin",
      () => `
<dictionary> {
  HTTPEnable : 0
  HTTPSEnable : 0
}
`,
    );

    expect(env.HTTP_PROXY).toBeUndefined();
    expect(env.HTTPS_PROXY).toBeUndefined();
    expect(env.NO_PROXY).toBeUndefined();
  });
});
