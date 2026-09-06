import { useCallback, useEffect, useState } from 'react';

/**
 * Where the studio is. One value replaces the separate flags that used to say
 * which page was showing and which workflow was open, so every view has an
 * address: it can be linked, reloaded, and reached with the back button.
 */
export type Route =
  | { name: 'workflows' }
  | { name: 'components' }
  | { name: 'editor'; workflowId: string };

export const WORKFLOWS: Route = { name: 'workflows' };
export const COMPONENTS: Route = { name: 'components' };

export function parse(pathname: string): Route {
  const segments = pathname.split('/').filter(Boolean);
  if (segments[0] === 'components') return COMPONENTS;
  if (segments[0] === 'workflows' && segments[1])
    return { name: 'editor', workflowId: decodeURIComponent(segments[1]) };
  return WORKFLOWS;
}

export function href(route: Route): string {
  switch (route.name) {
    case 'components':
      return '/components';
    case 'editor':
      return `/workflows/${encodeURIComponent(route.workflowId)}`;
    default:
      return '/';
  }
}

export function useRoute(): [Route, (next: Route, replace?: boolean) => void] {
  const [route, setRoute] = useState<Route>(() => parse(window.location.pathname));

  useEffect(() => {
    const onPopState = () => setRoute(parse(window.location.pathname));
    window.addEventListener('popstate', onPopState);
    return () => window.removeEventListener('popstate', onPopState);
  }, []);

  const navigate = useCallback((next: Route, replace = false) => {
    const target = href(next);
    if (target !== window.location.pathname) {
      window.history[replace ? 'replaceState' : 'pushState']({}, '', target);
    }
    setRoute(next);
  }, []);

  return [route, navigate];
}
