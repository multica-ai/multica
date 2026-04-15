/**
 * Hermes polyfills for APIs not natively supported.
 * This file is injected before Metro's module runtime exists, so it must not
 * import or require other modules.
 */

if (typeof globalThis.SharedArrayBuffer === "undefined") {
  // @ts-expect-error -- minimal shim to prevent ReferenceError
  globalThis.SharedArrayBuffer = ArrayBuffer;
}

function defineFalseGetter(prototype: object, property: string) {
  if (!Object.getOwnPropertyDescriptor(prototype, property)) {
    Object.defineProperty(prototype, property, {
      configurable: true,
      get: () => false,
    });
  }
}

defineFalseGetter(ArrayBuffer.prototype, "resizable");
defineFalseGetter(SharedArrayBuffer.prototype, "growable");

const stringPrototype = String.prototype as String & {
  toWellFormed?: () => string;
  isWellFormed?: () => boolean;
};

function toWellFormedString(value: string) {
  return value.replace(
    /[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?:^|[^\uD800-\uDBFF])[\uDC00-\uDFFF]/g,
    (match) => (match.length === 1 ? "\uFFFD" : `${match[0]}\uFFFD`),
  );
}

if (typeof stringPrototype.toWellFormed === "undefined") {
  Object.defineProperty(String.prototype, "toWellFormed", {
    configurable: true,
    value: function toWellFormed() {
      return toWellFormedString(String(this));
    },
  });
}

if (typeof stringPrototype.isWellFormed === "undefined") {
  Object.defineProperty(String.prototype, "isWellFormed", {
    configurable: true,
    value: function isWellFormed() {
      const value = String(this);
      return toWellFormedString(value) === value;
    },
  });
}
