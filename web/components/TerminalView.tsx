'use client';

import { useEffect, useRef, useState } from 'react';

export interface TerminalTheme {
  background: string;
  foreground: string;
  cursor: string;
  selectionBackground: string;
}

type Phase = 'connecting' | 'attached' | 'exited' | 'gone';

/**
 * A terminal living on the server. Attaching replays its scrollback; leaving detaches without
 * stopping the process. Keystrokes go up as bytes, output comes down as bytes, and size changes
 * go up as a resize message.
 */
export function TerminalView({ id, theme, fontFamily }: { id: string; theme: TerminalTheme; fontFamily: string }) {
  const host = useRef<HTMLDivElement>(null);
  const [phase, setPhase] = useState<Phase>('connecting');
  const [exitCode, setExitCode] = useState<number | null>(null);

  useEffect(() => {
    let disposed = false;
    let ws: WebSocket | undefined;
    let cleanup = () => {};

    void (async () => {
      // xterm touches the DOM at import; the page is prerendered without one.
      const [{ Terminal }, { FitAddon }] = await Promise.all([import('@xterm/xterm'), import('@xterm/addon-fit')]);
      // xterm measures the cell size once; measuring a fallback font misaligns every glyph.
      await document.fonts?.load(`13px ${fontFamily}`).catch(() => {});
      if (disposed || !host.current) return;
      const term = new Terminal({
        theme,
        fontFamily,
        fontSize: 13,
        lineHeight: 1.25,
        cursorBlink: true,
        scrollback: 5000,
        allowProposedApi: false,
      });
      const fit = new FitAddon();
      term.loadAddon(fit);
      term.open(host.current);
      fit.fit();

      const encoder = new TextEncoder();
      const sendSize = () => {
        if (ws?.readyState === WebSocket.OPEN) {
          ws.send(JSON.stringify({ type: 'resize', cols: term.cols, rows: term.rows }));
        }
      };
      const connect = () => {
        const scheme = location.protocol === 'https:' ? 'wss' : 'ws';
        ws = new WebSocket(`${scheme}://${location.host}/v1/terminals/${id}/stream`);
        ws.binaryType = 'arraybuffer';
        ws.onopen = () => {
          term.reset(); // the server replays the whole scrollback on every attach
          setPhase('attached');
          sendSize();
        };
        ws.onmessage = (m) => {
          if (typeof m.data === 'string') {
            const msg = JSON.parse(m.data) as { type: string; code?: number };
            if (msg.type === 'exit') {
              setExitCode(msg.code ?? null);
              setPhase('exited');
            }
            return;
          }
          term.write(new Uint8Array(m.data as ArrayBuffer));
        };
        ws.onclose = (c) => {
          if (disposed) return;
          if (c.code === 1013) {
            setTimeout(connect, 300); // fell behind; attach again
            return;
          }
          setPhase((p) => (p === 'exited' ? p : 'gone'));
        };
      };
      connect();

      const input = term.onData((data) => {
        if (ws?.readyState === WebSocket.OPEN) ws.send(encoder.encode(data));
      });
      const resized = term.onResize(sendSize);
      const observer = new ResizeObserver(() => fit.fit());
      observer.observe(host.current);
      term.focus();

      cleanup = () => {
        observer.disconnect();
        input.dispose();
        resized.dispose();
        ws?.close();
        term.dispose();
      };
    })();

    return () => {
      disposed = true;
      cleanup();
    };
    // The theme and font are fixed for the page's life.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id]);

  return (
    <div className="term-pane" data-phase={phase}>
      {/* xterm makes the element it opens in its own root and positions inside it absolutely,
          so the margin lives on a wrapper. */}
      <div className="term-screen">
        <div ref={host} className="term-host" />
      </div>
      {phase === 'exited' && <p className="term-note">Process exited{exitCode !== null ? ` with code ${exitCode}` : ''}.</p>}
      {phase === 'gone' && <p className="term-note">This terminal is closed.</p>}
    </div>
  );
}
