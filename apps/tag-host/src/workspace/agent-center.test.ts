// @vitest-environment node

import { describe, expect, it } from 'vitest';
import { agentCenterPath } from './agent-center';

describe('Agent center navigation', () => {
  it('places team configuration under Agents', () => {
    expect(agentCenterPath('studio-a', 'roles')).toBe('/studio-a/agents');
    expect(agentCenterPath('studio-a', 'teams')).toBe('/studio-a/agents/teams');
  });
});
