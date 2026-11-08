// One icon set, drawn on a 16px grid with a 1.6px stroke.

import type { ReactNode, SVGProps } from 'react';

function Icon({ children, ...props }: SVGProps<SVGSVGElement> & { children: ReactNode }) {
  return (
    <svg
      width="16"
      height="16"
      viewBox="0 0 16 16"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.6"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      focusable="false"
      {...props}
    >
      {children}
    </svg>
  );
}

export const ChevronRight = () => (
  <Icon className="icon chevron">
    <path d="M6 3.5 10.5 8 6 12.5" />
  </Icon>
);
export const Check = () => (
  <Icon>
    <path d="M3.5 8.5 6.5 11.5 12.5 4.5" />
  </Icon>
);
export const Cross = () => (
  <Icon>
    <path d="M4 4l8 8M12 4l-8 8" />
  </Icon>
);
export const Ban = () => (
  <Icon>
    <circle cx="8" cy="8" r="5.5" />
    <path d="M4.2 11.8 11.8 4.2" />
  </Icon>
);
/** Waiting on the owner: filled, so the state that needs a person is the heaviest mark. */
export const Waiting = () => (
  <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden="true" focusable="false">
    <circle cx="8" cy="8" r="7" fill="currentColor" />
    <path d="M6.2 6.3a1.8 1.8 0 1 1 2.5 1.6c-.45.2-.7.6-.7 1.05v.25" fill="none" stroke="#1a1204" strokeWidth="1.6" strokeLinecap="round" />
    <circle cx="8" cy="11.4" r=".95" fill="#1a1204" />
  </svg>
);
export const Plus = () => (
  <Icon>
    <path d="M8 3v10M3 8h10" />
  </Icon>
);
export const TerminalIcon = () => (
  <Icon>
    <path d="M3.5 5 6.5 8l-3 3M8.5 11.5h4" />
  </Icon>
);
export const Inbox = () => (
  <Icon>
    <path d="M2.5 9.5h3l1 1.5h3l1-1.5h3" />
    <path d="M4 3.5h8l1.5 6v3h-11v-3z" />
  </Icon>
);
export const Stop = () => (
  <Icon>
    <rect x="4" y="4" width="8" height="8" rx="1.2" />
  </Icon>
);
export const Bell = () => (
  <Icon>
    <path d="M4.5 11V7.5a3.5 3.5 0 0 1 7 0V11l1 1h-9z" />
    <path d="M6.8 13.5a1.4 1.4 0 0 0 2.4 0" />
  </Icon>
);
export const LogOut = () => (
  <Icon>
    <path d="M6.5 3H3.5v10h3" />
    <path className="i-move" d="M10 5l3 3-3 3M13 8H6.5" />
  </Icon>
);
export const PanelRight = () => (
  <Icon>
    <rect x="2.5" y="3" width="11" height="10" rx="1.5" />
    <path d="M9.5 3v10" />
  </Icon>
);
export const PanelLeft = () => (
  <Icon>
    <rect x="2.5" y="3" width="11" height="10" rx="1.5" />
    <path className="i-move" d="M6.5 3v10" />
  </Icon>
);
export const Pencil = () => (
  <Icon>
    <path d="M10.5 3.5 12.5 5.5 6 12H4v-2z" />
    <path d="M9 5l2 2" />
  </Icon>
);
export const Pin = () => (
  <Icon>
    <path d="M6 2.5h4M7 2.5v4L4.5 9h7L9 6.5v-4M8 9v4.5" />
  </Icon>
);
export const Archive = () => (
  <Icon>
    <rect x="2.5" y="3" width="11" height="3" rx="0.8" />
    <path d="M3.5 6v6.5h9V6" />
    <path className="i-move" d="M6.5 8.5h3" />
  </Icon>
);
export const Copy = () => (
  <Icon>
    <rect x="5.5" y="5.5" width="8" height="8" rx="1.5" />
    <path d="M10.5 5.5V3.5a1 1 0 0 0-1-1h-6a1 1 0 0 0-1 1v6a1 1 0 0 0 1 1h2" />
  </Icon>
);
export const Menu = () => (
  <Icon>
    <path d="M2.5 4.5h11M2.5 8h11M2.5 11.5h11" />
  </Icon>
);
export const ArrowLeft = () => (
  <Icon>
    <path d="M7 3.5 2.5 8 7 12.5M3 8h10.5" />
  </Icon>
);
export const Send = () => (
  <Icon>
    <path d="M8 13V3M3.5 7.5 8 3l4.5 4.5" />
  </Icon>
);

/** A step in progress: one eased turn a second. */
/** Something in progress: Orla's small rolling wave, never a turning ring. */
export const Spinner = () => <span className="wave spinner" aria-hidden="true" />;

export const Branch = () => (
  <Icon>
    <circle cx="5" cy="3.8" r="1.6" />
    <circle cx="5" cy="12.2" r="1.6" />
    <circle cx="11" cy="5.8" r="1.6" />
    <path d="M5 5.4v5.2M11 7.4c0 2.2-1.8 2.8-4.4 3.6" />
  </Icon>
);
export const Undo = () => (
  <Icon>
    <path d="M5.5 3.5 2.5 6.5l3 3" />
    <path d="M3 6.5h6.5a3.5 3.5 0 0 1 0 7H7" />
  </Icon>
);
export const Merge = () => (
  <Icon>
    <circle cx="4.5" cy="3.8" r="1.6" />
    <circle cx="4.5" cy="12.2" r="1.6" />
    <circle cx="11.5" cy="12.2" r="1.6" />
    <path d="M4.5 5.4v5.2M4.5 5.6c0 3 2.6 4.6 5.4 5.4" />
  </Icon>
);
export const Commit = () => (
  <Icon>
    <circle cx="8" cy="8" r="2.4" />
    <path d="M1.5 8h4.1M10.4 8h4.1" />
  </Icon>
);
export const Trash = () => (
  <Icon>
    <path className="i-lid" d="M3 4.5h10M6.5 4.5V3h3v1.5" />
    <path d="M4.5 4.5l.6 8.5h5.8l.6-8.5" />
  </Icon>
);
export const Star = () => (
  <Icon>
    <path d="M8 2.5l1.7 3.5 3.8.5-2.8 2.6.7 3.8L8 11.1l-3.4 1.8.7-3.8-2.8-2.6 3.8-.5z" />
  </Icon>
);
// GitHub's mark (from Simple Icons), filled rather than stroked like the rest of the set.
export const GitHubMark = () => (
  <svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true" focusable="false">
    <path d="M12 .297c-6.63 0-12 5.373-12 12 0 5.303 3.438 9.8 8.205 11.385.6.113.82-.258.82-.577 0-.285-.01-1.04-.015-2.04-3.338.724-4.042-1.61-4.042-1.61C4.422 18.07 3.633 17.7 3.633 17.7c-1.087-.744.084-.729.084-.729 1.205.084 1.838 1.236 1.838 1.236 1.07 1.835 2.809 1.305 3.495.998.108-.776.417-1.305.76-1.605-2.665-.3-5.466-1.332-5.466-5.93 0-1.31.465-2.38 1.235-3.22-.135-.303-.54-1.523.105-3.176 0 0 1.005-.322 3.3 1.23.96-.267 1.98-.399 3-.405 1.02.006 2.04.138 3 .405 2.28-1.552 3.285-1.23 3.285-1.23.645 1.653.24 2.873.12 3.176.765.84 1.23 1.91 1.23 3.22 0 4.61-2.805 5.625-5.475 5.92.42.36.81 1.096.81 2.22 0 1.606-.015 2.896-.015 3.286 0 .315.21.69.825.57C20.565 22.092 24 17.592 24 12.297c0-6.627-5.373-12-12-12" />
  </svg>
);
export const Paperclip = () => (
  <Icon>
    <path d="M13.2 7.6 8.3 12.5a3.2 3.2 0 0 1-4.5-4.5l5-5a2.1 2.1 0 0 1 3 3l-5 5a1 1 0 0 1-1.5-1.5l4.6-4.6" />
  </Icon>
);
export const FileIcon = () => (
  <Icon>
    <path d="M9 2H4.5A1.5 1.5 0 0 0 3 3.5v9A1.5 1.5 0 0 0 4.5 14h7a1.5 1.5 0 0 0 1.5-1.5V6L9 2Z" />
    <path d="M9 2v4h4" />
  </Icon>
);
export const VideoIcon = () => (
  <Icon>
    <rect x="2" y="4" width="9" height="8" rx="1.5" />
    <path d="m11 7 3-2v6l-3-2" />
  </Icon>
);
export const Fork = () => (
  <Icon>
    <circle cx="4.5" cy="3.5" r="1.5" />
    <circle cx="11.5" cy="3.5" r="1.5" />
    <circle cx="8" cy="12.5" r="1.5" />
    <path d="M4.5 5v1.5a2 2 0 0 0 2 2h3a2 2 0 0 0 2-2V5M8 8.5V11" />
  </Icon>
);
export const Bookmark = () => (
  <Icon>
    <path d="M4.5 2.5h7a.5.5 0 0 1 .5.5v10.5L8 11l-4 2.5V3a.5.5 0 0 1 .5-.5Z" />
  </Icon>
);
export const Settings = () => (
  <Icon>
    <path d="M2.5 4.5h6M11.5 4.5h2M2.5 11.5h2M7.5 11.5h6" />
    <circle cx="10" cy="4.5" r="1.5" />
    <circle cx="6" cy="11.5" r="1.5" />
  </Icon>
);
export const Gauge = () => (
  <Icon>
    <path d="M2.8 11.5a5.5 5.5 0 1 1 10.4 0" />
    <path d="M8 9.5 10.5 6" />
    <circle cx="8" cy="9.8" r="0.9" />
  </Icon>
);
export const FolderIcon = () => (
  <Icon>
    <path d="M2.5 4.5a1 1 0 0 1 1-1h3l1.5 1.5h4.5a1 1 0 0 1 1 1v6a1 1 0 0 1-1 1h-9a1 1 0 0 1-1-1v-7.5Z" />
  </Icon>
);
export const GitLabMark = () => (
  <svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true" focusable="false">
    <path d="m23.6 9.6-.03-.09-3.27-8.53a.85.85 0 0 0-1.62.06l-2.2 6.75H7.52L5.32 1.04A.85.85 0 0 0 3.7.98L.43 9.5l-.03.09a6.07 6.07 0 0 0 2.01 7.01l.01.01.03.02 4.98 3.73 2.47 1.87 1.5 1.13a1.01 1.01 0 0 0 1.22 0l1.5-1.13 2.47-1.87 5.02-3.75.01-.01a6.08 6.08 0 0 0 2.01-7Z" />
  </svg>
);
export const BitbucketMark = () => (
  <svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true" focusable="false">
    <path d="M.78 1.21a.77.77 0 0 0-.77.89l3.26 19.79c.08.5.51.87 1.02.87h15.64a.77.77 0 0 0 .77-.65l3.26-20a.77.77 0 0 0-.77-.89L.78 1.21Zm13.73 14.3H9.52l-1.35-7.06h7.55l-1.21 7.06Z" />
  </svg>
);
export const RepoIcon = () => (
  <Icon>
    <path d="M4.5 2.5h7.5a.5.5 0 0 1 .5.5v9.5h-8a1.5 1.5 0 0 0-1.5 1.5V4a1.5 1.5 0 0 1 1.5-1.5Z" />
    <path d="M3 14a1.5 1.5 0 0 1 1.5-1.5h8V14a.5.5 0 0 1-.5.5h-7.5A1.5 1.5 0 0 1 3 14Z" />
  </Icon>
);
export const AtSign = () => (
  <Icon>
    <circle cx="8" cy="8" r="2.6" />
    <path d="M10.6 8v1.1a1.9 1.9 0 0 0 3.8 0V8a6.4 6.4 0 1 0-2.5 5.1" />
  </Icon>
);
export const Sparkle = () => (
  <Icon>
    <path d="M8 2.5c.4 2.6 1.9 4.1 4.5 4.5-2.6.4-4.1 1.9-4.5 4.5-.4-2.6-1.9-4.1-4.5-4.5 2.6-.4 4.1-1.9 4.5-4.5Z" />
    <path d="M12.5 11.5v2M11.5 12.5h2" />
  </Icon>
);
export const TimelineIcon = () => (
  <Icon>
    <path d="M4 3.5v9" />
    <circle cx="4" cy="4.5" r="1.4" />
    <circle cx="4" cy="11.5" r="1.4" />
    <path d="M7.5 4.5h5M7.5 11.5h3.5M7.5 8h4" />
  </Icon>
);
export const DiffIcon = () => (
  <Icon>
    <path d="M5 2.5v6M2 5.5h6M2.5 12h6" />
    <path d="M11 3.5h2.5v10H8" />
  </Icon>
);
export const PuzzleIcon = () => (
  <Icon>
    <path d="M3 5.5h2.2a1.5 1.5 0 1 1 2.6 0H10v2.3a1.5 1.5 0 1 1 0 2.6V13H7.6a1.5 1.5 0 1 0-2.6 0H3V10.4a1.5 1.5 0 1 0 0-2.6V5.5Z" />
  </Icon>
);
export const Grip = () => (
  <Icon className="icon grip" stroke="none" fill="currentColor">
    <circle cx="6" cy="4" r="1.1" />
    <circle cx="10" cy="4" r="1.1" />
    <circle cx="6" cy="8" r="1.1" />
    <circle cx="10" cy="8" r="1.1" />
    <circle cx="6" cy="12" r="1.1" />
    <circle cx="10" cy="12" r="1.1" />
  </Icon>
);
export const FlowIcon = () => (
  <Icon>
    <rect x="2.5" y="2" width="5" height="3.5" rx="1" />
    <rect x="8.5" y="10.5" width="5" height="3.5" rx="1" />
    <path d="M5 5.5v2.5a1.5 1.5 0 0 0 1.5 1.5h3.5a1 1 0 0 1 1 1" />
  </Icon>
);
export const Clock = () => (
  <Icon>
    <circle cx="8" cy="8" r="5.6" />
    <path d="M8 5v3.2l2 1.3" />
  </Icon>
);
export const Bolt = () => (
  <Icon>
    <path d="M9 2 3.5 9H8l-1 5 5.5-7H8z" />
  </Icon>
);
export const Books = () => (
  <Icon>
    <rect x="2.5" y="3" width="3" height="10" rx="0.6" />
    <rect x="6.5" y="3" width="3" height="10" rx="0.6" />
    <path d="m10.6 3.6 2.6-.7 2 9.3-2.6.7z" />
  </Icon>
);
