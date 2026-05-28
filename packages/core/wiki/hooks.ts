import { useCoreQuery } from "../provider";
import {
  wikiPageActivityOptions,
  wikiPageDetailOptions,
  wikiPageListOptions,
} from "./queries";

export function useWikiPageList(wsId: string) {
  return useCoreQuery(wikiPageListOptions(wsId));
}

export function useWikiPageDetail(wsId: string, pageId: string | null) {
  return useCoreQuery(wikiPageDetailOptions(wsId, pageId));
}

export function useWikiPageActivity(wsId: string, pageId: string | null) {
  return useCoreQuery(wikiPageActivityOptions(wsId, pageId));
}
