// @vitest-environment node

import { describe, expect, it } from 'vitest';
import path from 'node:path';
import config from './vite.config';

describe('Tag Host Vite contract', () => {
  it('mounts the development websocket beneath the gateway Tag path', () => {
    expect(config).toMatchObject({
      base: '/tag/',
      server: {
        hmr: {
          path: '/',
        },
      },
    });

    expect(path.posix.join(config.base ?? '/', config.server?.hmr?.path ?? '/')).toBe(
      '/tag/',
    );
  });
});
