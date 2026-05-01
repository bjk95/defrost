import { useMemo } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import type { Decorator } from "@storybook/react-vite";

type Body = unknown;

export interface MockHandler {
  match: (url: string) => boolean;
  body: Body | ((url: string) => Body);
  status?: number;
}

// `Infinity` is a valid number in JS but becomes `null` when serialised through
// JSON. Histogram fixtures use `Infinity` for the open-ended top bucket; the
// real server emits `null`, which `normalizeWireMetrics` converts back to
// `Infinity`. Going through `serialize` keeps the story round-trip identical to
// production.
function serialize(body: Body): string {
  return JSON.stringify(body, (_k, v) =>
    typeof v === "number" && !isFinite(v) ? null : v,
  );
}

// Captured at module load — every install delegates here for unmatched URLs.
const ORIGINAL_FETCH: typeof fetch = globalThis.fetch.bind(globalThis);

export function installMockFetch(handlers: MockHandler[]): void {
  globalThis.fetch = async (input: RequestInfo | URL): Promise<Response> => {
    const url =
      typeof input === "string"
        ? input
        : input instanceof URL
          ? input.toString()
          : input.url;
    for (const h of handlers) {
      if (h.match(url)) {
        const data = typeof h.body === "function" ? (h.body as (u: string) => Body)(url) : h.body;
        return new Response(serialize(data), {
          status: h.status ?? 200,
          headers: { "content-type": "application/json" },
        });
      }
    }
    return ORIGINAL_FETCH(input as RequestInfo);
  };
}

// Decorator that scopes a fresh QueryClient *and* installs fetch mocks for the
// duration of one story. A fresh client per story is important because
// react-query caches by key — without it, switching stories wouldn't refetch
// against the new mock data.
//
// `installMockFetch` runs synchronously on every render so the very first
// useQuery fires against the patched fetch. Repeat installs are idempotent
// because each one rewires from the module-level original.
//
// When `initialEntries` is provided, an inner MemoryRouter takes over routing
// for the story so navigation hooks (`useNavigate`, `useSearchParams`) see the
// expected URL. This nested router shadows the global one from preview.tsx.
export function withMockApi(
  handlers: MockHandler[],
  initialEntries?: string[],
): Decorator {
  return function MockApiDecorator(Story) {
    installMockFetch(handlers);
    const client = useMemo(
      () =>
        new QueryClient({
          defaultOptions: { queries: { retry: false, gcTime: Infinity, staleTime: 0 } },
        }),
      [],
    );
    const tree = (
      <QueryClientProvider client={client}>
        <Story />
      </QueryClientProvider>
    );
    if (!initialEntries) return tree;
    return <MemoryRouter initialEntries={initialEntries}>{tree}</MemoryRouter>;
  };
}

export const matchPath = (path: string) => (url: string) => url.includes(path);
