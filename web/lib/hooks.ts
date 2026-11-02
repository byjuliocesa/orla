'use client';

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { api, Unauthorized } from './api';
import { openStream } from './sse';
import { buildTimeline, type Timeline } from './timeline';
import type { OrlaEvent, PendingApproval, Session, TerminalInfo } from './types';

export function toLogin() {
  if (typeof window !== 'undefined' && !window.location.pathname.startsWith('/login')) {
    window.location.assign('/login/');
  }
}

export interface Overview {
  sessions: Session[];
  approvals: PendingApproval[];
  terminals: TerminalInfo[];
  /** False while the live stream is down; lists may then be stale. */
  live: boolean;
  loaded: boolean;
  refresh: () => void;
}

/**
 * Sessions, pending requests and terminals, kept current by the stream of every session's
 * events: any event schedules one reload of the lists, so they always come from the server's
 * own reading of the state.
 */
export function useOverview(): Overview {
  const [sessions, setSessions] = useState<Session[]>([]);
  const [approvals, setApprovals] = useState<PendingApproval[]>([]);
  const [terminals, setTerminals] = useState<TerminalInfo[]>([]);
  const [live, setLive] = useState(false);
  const [loaded, setLoaded] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);

  const load = useCallback(async () => {
    try {
      const [s, a, t] = await Promise.all([api.sessions(), api.approvals(), api.terminals()]);
      s.sort((x, y) => (y.updated ?? y.created).localeCompare(x.updated ?? x.created));
      setSessions(s);
      setApprovals(a);
      setTerminals(t);
      setLoaded(true);
    } catch (err) {
      if (err instanceof Unauthorized) toLogin();
    }
  }, []);

  const refresh = useCallback(() => {
    clearTimeout(timer.current);
    timer.current = setTimeout(() => void load(), 150);
  }, [load]);

  useEffect(() => {
    const stop = openStream(() => '/v1/events', {
      onOpen: () => {
        setLive(true);
        void load(); // whatever happened while the stream was down
      },
      onDown: () => setLive(false),
      onEvent: refresh,
      onUnauthorized: toLogin,
    });
    // Terminals report no events; a process that exits shows on the next look.
    const poll = setInterval(() => void api.terminals().then(setTerminals).catch(() => {}), 15_000);
    return () => {
      stop();
      clearInterval(poll);
      clearTimeout(timer.current);
    };
  }, [load, refresh]);

  return { sessions, approvals, terminals, live, loaded, refresh };
}

/** Every event of one session, replayed then live, resuming after the last one seen. */
export function useSessionTimeline(session: string | undefined): { timeline: Timeline; events: OrlaEvent[]; live: boolean } {
  const [events, setEvents] = useState<OrlaEvent[]>([]);
  const [live, setLive] = useState(false);

  useEffect(() => {
    setEvents([]);
    if (!session) return;
    let last = 0;
    let batch: OrlaEvent[] = [];
    let frame = 0;
    const flush = () => {
      frame = 0;
      const add = batch;
      batch = [];
      setEvents((prev) => prev.concat(add));
    };
    const stop = openStream(() => `/v1/sessions/${session}/events?after=${last}`, {
      onOpen: () => setLive(true),
      onDown: () => setLive(false),
      onUnauthorized: toLogin,
      onEvent: (e) => {
        if (e.seq <= last) return;
        last = e.seq;
        batch.push(e);
        // A replay arrives as hundreds of events at once; render them once per frame.
        if (!frame) frame = requestAnimationFrame(flush);
      },
    });
    return () => {
      stop();
      if (frame) cancelAnimationFrame(frame);
    };
  }, [session]);

  const timeline = useMemo(() => buildTimeline(events), [events]);
  return { timeline, events, live };
}
