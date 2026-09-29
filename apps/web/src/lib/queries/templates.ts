import { useQuery } from "@tanstack/react-query";
import { getMarkdownTemplates } from "@/lib/api";
import { keys } from "./keys";

/**
 * The templates a new markdown document can start from, in `locale`. They
 * ship with the server and change only with a release, so they are fetched
 * once per session and language.
 */
export function useMarkdownTemplates(locale: string, enabled: boolean) {
  return useQuery({
    queryKey: keys.markdownTemplates(locale),
    queryFn: () => getMarkdownTemplates(locale),
    enabled,
    staleTime: Infinity,
  });
}
