// @vitest-environment node

import { describe, expect, it } from 'vitest';
import config from './vite.config';

describe('Tag Host Vite contract', () => {
  it('owns the unified :3100 browser entry and keeps HMR beneath /tag', () => {
    expect(config).toMatchObject({
      base: '/tag/',
      server: {
        host: '127.0.0.1',
        port: 3100,
        strictPort: true,
        hmr: {
          path: '/tag/__vite_hmr',
        },
      },
    });

    expect(config.server?.hmr?.path).toBe('/tag/__vite_hmr');

    expect(
      config.plugins
        ?.flat()
        .filter(Boolean)
        .map((plugin) =>
          typeof plugin === 'object' && 'name' in plugin ? plugin.name : null,
        ),
    ).toContain('vibes-tag-unified-gateway');
  });
});
