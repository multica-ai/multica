import { QueryClient } from "@tanstack/react-query";

function createAppQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: {
        staleTime: 30 * 1000,
        gcTime: 10 * 60 * 1000,
        refetchOnWindowFocus: false,
        retry: 1,
      },
      mutations: {
        retry: 0,
      },
    },
  });
}

let appQueryClient: QueryClient | null = null;

export function getAppQueryClient(): QueryClient {
  if (!appQueryClient) {
    appQueryClient = createAppQueryClient();
  }

  return appQueryClient;
}
