import "i18next";

// Local slice of the i18next augmentation that owns the `ui` namespace.
// The base augmentation lives in packages/views/i18n/resources-types.ts and
// declares everything else; this file contributes only the `ui` entry via
// declaration merging on the global `I18nResources` interface so
// packages/ui can typecheck the selector form standalone without depending
// on @multica/views.
//
// When both files are loaded together (in a consumer's typecheck program),
// the two augmentations compose: views contributes common/auth/... and ui
// contributes `ui`. No properties overlap, so the merge is conflict-free.
//
// The resource shape is mirrored from packages/views/locales/{en,zh-Hans}/ui.json.
// Drift between the JSON and these types is not caught by the locale parity
// test — if you add a key to ui.json, mirror it here.
declare global {
  interface I18nResources {
    ui: {
      attach_file: string;
      toggle_sidebar: string;
      pagination_previous: string;
      pagination_next: string;
      copy_code: string;
      plain_text: string;
    };
  }
}

declare module "i18next" {
  interface CustomTypeOptions {
    resources: I18nResources;
    enableSelector: true;
  }
}
