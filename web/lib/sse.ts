import type { OrlaEvent } from './types';

export interface StreamHandlers {
  onEvent: (e: OrlaEvent) => void;
  /** Called on every (re)connection, before its first event. */
  onOpen?: () => void;
  onDown?: () => void;
  onUnauthorized?: () => void;
}

const maxBackoff = 10_000;

/**
 * Reads an Orla event stream over a WebSocket, which carries the login cookie and, unlike an SSE
 * response, does not hold one of the six connections a browser allows per site: with SSE a few
 * open tabs used them all and a reloaded tab waited for its own page. It reconnects until
 * stopped; `url` is asked again on each attempt, so a session stream resumes after the last seq
 * it delivered.
 */
export function openStream(url: () => string, h: StreamHandlers): () => void {
  let stopped = false;
  let socket: WebSocket | null = null;
  let retry: ReturnType<typeof setTimeout> | undefined;
  let backoff = 1000;

  const connect = () => {
    if (stopped) return;
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
    const ws = new WebSocket(`${proto}//${location.host}${url()}`);
    socket = ws;
    let opened = false;
    ws.onopen = () => {
      opened = true;
      backoff = 1000;
      h.onOpen?.();
    };
    ws.onmessage = (m) => {
      if (typeof m.data !== 'string') return;
      try {
        h.onEvent(JSON.parse(m.data) as OrlaEvent);
      } catch {
        // not an event
      }
    };
    ws.onclose = () => {
      if (stopped) return;
      h.onDown?.();
      // A handshake refused before it opened may be a login that ended; the API says which.
      const check = opened
        ? Promise.resolve(false)
        : fetch('/v1/approvals', { credentials: 'same-origin' }).then(
            (r) => r.status === 401,
            () => false,
          );
      void check.then((unauthorized) => {
        if (stopped) return;
        if (unauthorized) return h.onUnauthorized?.();
        retry = setTimeout(connect, backoff);
        backoff = Math.min(backoff * 2, maxBackoff);
      });
    };
  };

  connect();
  return () => {
    stopped = true;
    clearTimeout(retry);
    socket?.close();
  };
}
