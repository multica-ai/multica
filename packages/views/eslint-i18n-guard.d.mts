// `eslint-i18n-guard.mjs` stays plain JS so eslint.config.mjs can import it
// without a TS loader; this declares its shape for eslint-i18n-guard.test.ts.
// The shape is ESLint's `no-restricted-syntax` object form.
export declare const NO_UNTRANSLATED_ATTRIBUTES: {
  selector: string;
  message: string;
};
export declare const NO_UNTRANSLATED_TOAST: {
  selector: string;
  message: string;
};
