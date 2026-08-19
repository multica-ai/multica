// @vitest-environment node

import { describe, expect, it } from 'vitest';
import { agentCenterPath, remapAgentCenterPath } from './agent-center';

describe('Agent center navigation', () => {
  it('places team configuration under Agents', () => {
    expect(agentCenterPath('studio-a', 'roles')).toBe('/studio-a/agents');
    expect(agentCenterPath('studio-a', 'teams')).toBe('/studio-a/agents/teams');
    expect(remapAgentCenterPath('/studio-a/squads')).toBe(
      '/studio-a/agents/teams',
    );
    expect(remapAgentCenterPath('/studio-a/squads/team-1?view=members')).toBe(
      '/studio-a/agents/teams/team-1?view=members',
    );
  });

  it('keeps approved manual creation and unrelated paths stable', () => {
    expect(remapAgentCenterPath('/studio-a/agents/new?squad=team-1')).toBe(
      '/studio-a/agents/new/manual?squad=team-1',
    );
    expect(remapAgentCenterPath('/studio-a/issues/task-1')).toBe(
      '/studio-a/issues/task-1',
    );
  });
});
