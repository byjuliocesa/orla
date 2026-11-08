import { useCallback, useEffect, useState } from 'react';

// The export has one page; where it shows lives in the query string, so links and the back
// button work without a route per session id, which a static export cannot have.

export type View = 'home' | 'session' | 'approvals' | 'terminals' | 'new' | 'library';

export interface Route {
  view: View;
  session?: string;
  terminal?: string;
}

function parse(search: string): Route {
  const q = new URLSearchParams(search);
  const session = q.get('s') ?? undefined;
  const terminal = q.get('t') ?? undefined;
  const v = q.get('v');
  const view: View =
    v === 'approvals' || v === 'terminals' || v === 'new' || v === 'library' ? v : session ? 'session' : 'home';
  return { view, session, terminal };
}

function format(r: Route): string {
  const q = new URLSearchParams();
  if (r.view !== 'session' && r.view !== 'home') q.set('v', r.view);
  if (r.session) q.set('s', r.session);
  if (r.terminal) q.set('t', r.terminal);
  const s = q.toString();
  return s ? `/?${s}` : '/';
}

export function useRoute(): [Route, (next: Route, replace?: boolean) => void] {
  const [route, setRoute] = useState<Route>({ view: 'home' });

  useEffect(() => {
    const read = () => setRoute(parse(window.location.search));
    read();
    window.addEventListener('popstate', read);
    return () => window.removeEventListener('popstate', read);
  }, []);

  const go = useCallback((next: Route, replace = false) => {
    const url = format(next);
    if (replace) window.history.replaceState(null, '', url);
    else window.history.pushState(null, '', url);
    setRoute(next);
  }, []);

  return [route, go];
}
