import "i18next";

declare global {
  // No application-specific interface is needed: mobile call sites
  // intentionally use string keys because navigation option objects,
  // item maps, and enum-label maps resolve keys dynamically.
}

declare module "i18next" {
  interface CustomTypeOptions {
    defaultNS: "common";
    resources: Record<string, Record<string, unknown>>;
    enableSelector: false;
  }
}
