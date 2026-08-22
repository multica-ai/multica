import { describe, expect, it } from "vitest";

import { getNativeNetworkConfig } from "../app.config";

describe("getNativeNetworkConfig", () => {
  it.each([undefined, "", "https://api.example.test"])(
    "keeps native cleartext access disabled for %s",
    (apiUrl) => {
      expect(getNativeNetworkConfig(apiUrl)).toEqual({
        androidUsesCleartextTraffic: false,
      });
    },
  );

  it.each([
    "http://192.168.1.42:8080",
    "http://localhost:8080",
    "http://multica.local:8080",
    "http://multica-server:8080",
    "http://[fd00::42]:8080",
  ])("enables narrow local-network access for %s", (apiUrl) => {
    expect(getNativeNetworkConfig(apiUrl)).toEqual({
      androidUsesCleartextTraffic: true,
      iosInfoPlist: {
        NSAppTransportSecurity: {
          NSAllowsLocalNetworking: true,
        },
        NSLocalNetworkUsageDescription:
          "Allow Multica to connect to your self-hosted server on the local network.",
      },
    });
  });

  it("enables a host-scoped ATS exception for a non-local HTTP server", () => {
    expect(getNativeNetworkConfig("http://api.example.test:8080")).toEqual({
      androidUsesCleartextTraffic: true,
      iosInfoPlist: {
        NSAppTransportSecurity: {
          NSAllowsLocalNetworking: true,
          NSExceptionDomains: {
            "api.example.test": {
              NSExceptionAllowsInsecureHTTPLoads: true,
              NSIncludesSubdomains: false,
            },
          },
        },
        NSLocalNetworkUsageDescription:
          "Allow Multica to connect to your self-hosted server on the local network.",
      },
    });
  });

  it.each([
    "http://203.0.113.42:8080",
    "http://[2001:db8::42]:8080",
  ])(
    "does not create an invalid ATS domain exception for IP address %s",
    (apiUrl) => {
      expect(getNativeNetworkConfig(apiUrl)).toEqual({
        androidUsesCleartextTraffic: true,
        iosInfoPlist: {
          NSAppTransportSecurity: {
            NSAllowsLocalNetworking: true,
          },
          NSLocalNetworkUsageDescription:
            "Allow Multica to connect to your self-hosted server on the local network.",
        },
      });
    },
  );

  it("does not weaken native networking for an invalid URL", () => {
    expect(getNativeNetworkConfig("not-a-url")).toEqual({
      androidUsesCleartextTraffic: false,
    });
  });
});
