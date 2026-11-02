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
 * Reads an Orla SSE stream with fetch, which carries the login cookie like EventSource does but
 * sees every event whatever its name. It reconnects until stopped; `url` is asked again on each
 * attempt, so a session stream resumes after the last seq it delivered.
 */
export function openStream(url: () => string, h: StreamHandlers): () => void {
  const stop = new AbortController();
  let backoff = 1000;

  const run = async () => {
    while (!stop.signal.aborted) {
      try {
        const res = await fetch(url(), {
          credentials: 'same-origin',
          headers: { Accept: 'text/event-stream' },
          signal: stop.signal,
        });
        if (res.status === 401) {
          h.onUnauthorized?.();
          return;
        }
        if (!res.ok || !res.body) throw new Error(`stream ${res.status}`);
        backoff = 1000;
        h.onOpen?.();
        await read(res.body, h.onEvent);
      } catch {
        if (stop.signal.aborted) return;
      }
      h.onDown?.();
      await sleep(backoff, stop.signal);
      backoff = Math.min(backoff * 2, maxBackoff);
    }
  };
  void run();
  return () => stop.abort();
}

async function read(body: ReadableStream<Uint8Array>, onEvent: (e: OrlaEvent) => void) {
  const reader = body.getReader();
  const decoder = new TextDecoder();
  let buffer = '';
  for (;;) {
    const { value, done } = await reader.read();
    if (done) return;
    buffer += decoder.decode(value, { stream: true });
    let end: number;
    while ((end = buffer.indexOf('\n\n')) >= 0) {
      const block = buffer.slice(0, end);
      buffer = buffer.slice(end + 2);
      const data = block
        .split('\n')
        .filter((line) => line.startsWith('data: '))
        .map((line) => line.slice(6))
        .join('\n');
      if (data) onEvent(JSON.parse(data) as OrlaEvent);
    }
  }
}

function sleep(ms: number, signal: AbortSignal) {
  return new Promise<void>((resolve) => {
    const t = setTimeout(resolve, ms);
    signal.addEventListener('abort', () => {
      clearTimeout(t);
      resolve();
    });
  });
}
