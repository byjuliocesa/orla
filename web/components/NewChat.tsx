import { useEffect, useRef, useState } from 'react';
import { api } from '@/lib/api';
import { ArrowLeft, PanelRight } from './Icons';
import { Mark } from './Logo';
import { PromptBox, type Outgoing } from './PromptBox';
import { Tooltip } from './Tooltip';
import { Typed } from './Typed';

// A new session is an empty chat: the first prompt and Claude Code's own choices. It starts in
// the server's home directory, and Claude Code makes the project's folder itself.

const PROMPTS = ['What are we building today?', 'Where do the agents go next?', 'Start something new.', 'What should the agent take on?'];

export function NewChat({
  started,
  onBack,
  dockOn,
  toggleDock,
  canDock,
}: {
  started: (id: string) => void;
  onBack: () => void;
  dockOn: boolean;
  toggleDock: () => void;
  canDock: boolean;
}) {
  const [hello] = useState(() => PROMPTS[Math.floor(Math.random() * PROMPTS.length)]!);
  const [sending, setSending] = useState(false);
  // Leaving the page while it sends must not pull the owner back to the new session.
  const alive = useRef(true);
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);

  const send = async (o: Outgoing) => {
    const reduce = matchMedia('(prefers-reduced-motion: reduce)').matches;
    const s = await api.createSession(''); // the server's home directory
    await api.startRun(s.id, {
      prompt: o.prompt,
      attachments: o.attachments,
      ultracode: o.ultracode,
      model: o.model,
      effort: o.effort,
      permission_mode: o.mode,
    });
    // The box floods once this returns; the session opens under the water.
    setTimeout(() => alive.current && started(s.id), reduce ? 0 : 1070);
  };

  return (
    <>
      <div className="topbar">
        <button className="icon-btn" onClick={onBack} aria-label="All sessions">
          <ArrowLeft />
        </button>
        <span className="title">New session</span>
      </div>
      <div className="nc" data-sending={sending || undefined}>
        {canDock && (
          <div className="nc-tools">
            <Tooltip label={dockOn ? 'Hide terminals' : 'Show terminals'}>
              <button className="icon-btn" onClick={toggleDock} aria-label={dockOn ? 'Hide terminals' : 'Show terminals'} aria-pressed={dockOn}>
                <PanelRight />
              </button>
            </Tooltip>
          </div>
        )}
        <div className="nc-stack">
          <div className="nc-hero">
            <span className="nc-mark">
              <Mark size={48} />
            </span>
            <h1 className="nc-hello">
              <Typed text={hello} fromStart speed={42} />
            </h1>
          </div>
          <PromptBox draftKey="orla.new-chat.draft" placeholder="Describe the task, or the project to start" onSend={send} flood onSending={setSending} />
        </div>
      </div>
    </>
  );
}
