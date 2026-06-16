import { afterEach, describe, expect, it, vi } from "vitest";
import { createLogger, getLogLevel, noopLogger, setLogLevel } from "./logger";

const store: Record<string, string> = {};

function stubLocalStorage() {
  vi.stubGlobal("window", {
    localStorage: {
      getItem: (key: string) => (key in store ? store[key] : null),
      setItem: (key: string, value: string) => {
        store[key] = value;
      },
      removeItem: (key: string) => {
        delete store[key];
      },
    },
  });
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  Object.keys(store).forEach((key) => delete store[key]);
});

describe("createLogger", () => {
  it("prints info, warn and error by default in a node environment", () => {
    const debugSpy = vi.spyOn(console, "log").mockImplementation(() => {});
    const infoSpy = vi.spyOn(console, "info").mockImplementation(() => {});
    const warnSpy = vi.spyOn(console, "warn").mockImplementation(() => {});
    const errorSpy = vi.spyOn(console, "error").mockImplementation(() => {});

    const logger = createLogger("test");
    logger.debug("debug msg");
    logger.info("info msg");
    logger.warn("warn msg");
    logger.error("error msg");

    expect(debugSpy).not.toHaveBeenCalled();
    expect(infoSpy).toHaveBeenCalledTimes(1);
    expect(warnSpy).toHaveBeenCalledTimes(1);
    expect(errorSpy).toHaveBeenCalledTimes(1);
  });

  it("respects 'off' and prints nothing", () => {
    stubLocalStorage();
    setLogLevel("off");

    const debugSpy = vi.spyOn(console, "log").mockImplementation(() => {});
    const infoSpy = vi.spyOn(console, "info").mockImplementation(() => {});
    const warnSpy = vi.spyOn(console, "warn").mockImplementation(() => {});
    const errorSpy = vi.spyOn(console, "error").mockImplementation(() => {});

    const logger = createLogger("test");
    logger.debug("debug msg");
    logger.info("info msg");
    logger.warn("warn msg");
    logger.error("error msg");

    expect(debugSpy).not.toHaveBeenCalled();
    expect(infoSpy).not.toHaveBeenCalled();
    expect(warnSpy).not.toHaveBeenCalled();
    expect(errorSpy).not.toHaveBeenCalled();
  });

  it("respects 'warn' and only prints warn and error", () => {
    stubLocalStorage();
    setLogLevel("warn");

    const debugSpy = vi.spyOn(console, "log").mockImplementation(() => {});
    const infoSpy = vi.spyOn(console, "info").mockImplementation(() => {});
    const warnSpy = vi.spyOn(console, "warn").mockImplementation(() => {});
    const errorSpy = vi.spyOn(console, "error").mockImplementation(() => {});

    const logger = createLogger("test");
    logger.debug("debug msg");
    logger.info("info msg");
    logger.warn("warn msg");
    logger.error("error msg");

    expect(debugSpy).not.toHaveBeenCalled();
    expect(infoSpy).not.toHaveBeenCalled();
    expect(warnSpy).toHaveBeenCalledTimes(1);
    expect(errorSpy).toHaveBeenCalledTimes(1);
  });

  it("respects 'error' and only prints error", () => {
    stubLocalStorage();
    setLogLevel("error");

    const debugSpy = vi.spyOn(console, "log").mockImplementation(() => {});
    const infoSpy = vi.spyOn(console, "info").mockImplementation(() => {});
    const warnSpy = vi.spyOn(console, "warn").mockImplementation(() => {});
    const errorSpy = vi.spyOn(console, "error").mockImplementation(() => {});

    const logger = createLogger("test");
    logger.debug("debug msg");
    logger.info("info msg");
    logger.warn("warn msg");
    logger.error("error msg");

    expect(debugSpy).not.toHaveBeenCalled();
    expect(infoSpy).not.toHaveBeenCalled();
    expect(warnSpy).not.toHaveBeenCalled();
    expect(errorSpy).toHaveBeenCalledTimes(1);
  });

  it("respects 'debug' and prints everything", () => {
    stubLocalStorage();
    setLogLevel("debug");

    const debugSpy = vi.spyOn(console, "log").mockImplementation(() => {});
    const infoSpy = vi.spyOn(console, "info").mockImplementation(() => {});
    const warnSpy = vi.spyOn(console, "warn").mockImplementation(() => {});
    const errorSpy = vi.spyOn(console, "error").mockImplementation(() => {});

    const logger = createLogger("test");
    logger.debug("debug msg");
    logger.info("info msg");
    logger.warn("warn msg");
    logger.error("error msg");

    expect(debugSpy).toHaveBeenCalledTimes(1);
    expect(infoSpy).toHaveBeenCalledTimes(1);
    expect(warnSpy).toHaveBeenCalledTimes(1);
    expect(errorSpy).toHaveBeenCalledTimes(1);
  });

  it("falls back to default when localStorage contains an invalid value", () => {
    stubLocalStorage();
    store["multica_log_level"] = "nonsense";

    const infoSpy = vi.spyOn(console, "info").mockImplementation(() => {});
    const logger = createLogger("test");
    logger.info("info msg");

    expect(infoSpy).toHaveBeenCalledTimes(1);
    expect(getLogLevel()).toBe("info");
  });

  it("applies level changes immediately to existing loggers", () => {
    stubLocalStorage();
    setLogLevel("info");

    const infoSpy = vi.spyOn(console, "info").mockImplementation(() => {});
    const logger = createLogger("test");
    logger.info("first");
    expect(infoSpy).toHaveBeenCalledTimes(1);

    setLogLevel("warn");
    logger.info("second");
    expect(infoSpy).toHaveBeenCalledTimes(1);
  });
});

describe("getLogLevel / setLogLevel", () => {
  it("reads and writes the level from localStorage", () => {
    stubLocalStorage();
    expect(getLogLevel()).toBe("info");

    setLogLevel("warn");
    expect(getLogLevel()).toBe("warn");
    expect(store["multica_log_level"]).toBe("warn");
  });

  it("does not throw when localStorage is unavailable", () => {
    vi.stubGlobal("window", {
      localStorage: {
        getItem: () => {
          throw new Error("disabled");
        },
        setItem: () => {
          throw new Error("disabled");
        },
      },
    });

    expect(() => setLogLevel("off")).not.toThrow();
    expect(getLogLevel()).toBe("info");
  });
});

describe("noopLogger", () => {
  it("never writes to console", () => {
    const debugSpy = vi.spyOn(console, "log").mockImplementation(() => {});
    const infoSpy = vi.spyOn(console, "info").mockImplementation(() => {});
    const warnSpy = vi.spyOn(console, "warn").mockImplementation(() => {});
    const errorSpy = vi.spyOn(console, "error").mockImplementation(() => {});

    noopLogger.debug("a");
    noopLogger.info("b");
    noopLogger.warn("c");
    noopLogger.error("d");

    expect(debugSpy).not.toHaveBeenCalled();
    expect(infoSpy).not.toHaveBeenCalled();
    expect(warnSpy).not.toHaveBeenCalled();
    expect(errorSpy).not.toHaveBeenCalled();
  });
});
