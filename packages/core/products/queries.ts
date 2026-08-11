import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const productMapKeys = {
  all: (wsId: string) => ["product-map", wsId] as const,
  tree: (wsId: string) => [...productMapKeys.all(wsId), "tree"] as const,
  node: (wsId: string, nodeId: string) =>
    [...productMapKeys.all(wsId), "node", nodeId] as const,
};

export function productMapTreeOptions(wsId: string) {
  return queryOptions({
    queryKey: productMapKeys.tree(wsId),
    queryFn: () => api.listProductMap(),
  });
}

export function productMapNodeOptions(wsId: string, nodeId: string) {
  return queryOptions({
    queryKey: productMapKeys.node(wsId, nodeId),
    queryFn: () => api.getProductMapNode(nodeId),
  });
}
