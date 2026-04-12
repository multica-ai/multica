import { useQuery } from '@tanstack/react-query';
import { api } from '../lib/api';

export const useConfig = () =>
  useQuery({
    queryKey: ['config'],
    queryFn: () => api.getConfig(),
    refetchInterval: 60000,
  });

export const useHealth = () =>
  useQuery({
    queryKey: ['health'],
    queryFn: async () => {
      const h = await api.checkHealth();
      return h !== null;
    },
    refetchInterval: 10000,
  });

export const useUser = () =>
  useQuery({
    queryKey: ['user'],
    queryFn: () => api.getCurrentUser(),
    refetchInterval: 60000,
  });

export const useWorkspaces = () =>
  useQuery({
    queryKey: ['workspaces'],
    queryFn: async () => {
      const ws = await api.getWorkspaces();
      return ws || [];
    },
    refetchInterval: 30000,
  });

export const useDaemon = () =>
  useQuery({
    queryKey: ['daemon'],
    queryFn: async () => {
      try {
        return await api.daemonStatus();
      } catch {
        return null;
      }
    },
    refetchInterval: 5000,
  });

export const useWorkspaceData = (workspaces: any[] = []) =>
  useQuery({
    queryKey: ['workspaceData', workspaces.map(w => w.id)],
    queryFn: async () => {
      if (!workspaces || workspaces.length === 0) return { runtimes: [], agents: [], issues: [], inbox: [] };

      const results = await Promise.all(
        workspaces.map(async (ws: any) => {
          const wsId = ws.id;
          const wsName = ws.name;
          const tag = (arr: any[]) => (arr || []).map(x => ({ ...x, workspace: wsName }));

          const [rt, ag, iss, ib] = await Promise.all([
            api.getRuntimes(wsId),
            api.getAgents(wsId),
            api.getIssues(wsId),
            api.getInbox(wsId),
          ]);

          return {
            runtimes: tag(rt as any[]),
            agents: tag(ag as any[]),
            issues: tag(((iss as any)?.issues || []) as any[]),
            inbox: tag(ib as any[]),
          };
        })
      );

      return {
        runtimes: results.flatMap(r => r.runtimes),
        agents: results.flatMap(r => r.agents),
        issues: results.flatMap(r => r.issues),
        inbox: results.flatMap(r => r.inbox),
      };
    },
    enabled: workspaces.length > 0,
    refetchInterval: 15000,
  });
