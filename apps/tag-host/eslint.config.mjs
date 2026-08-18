import reactConfig from '@multica/eslint-config/react';

export default [
  ...reactConfig,
  { ignores: ['dist/', 'src/routeTree.gen.ts'] },
  {
    files: ['**/*.test.{ts,tsx}'],
    rules: {
      'react/display-name': 'off',
    },
  },
];
