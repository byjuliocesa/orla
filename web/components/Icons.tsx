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
    <path d="M6.5 3H3.5v10h3M10 5l3 3-3 3M13 8H6.5" />
  </Icon>
);
export const PanelRight = () => (
  <Icon>
    <rect x="2.5" y="3" width="11" height="10" rx="1.5" />
    <path d="M9.5 3v10" />
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
export const Spinner = () => (
  <svg className="icon spinner" width="16" height="16" viewBox="0 0 16 16" aria-hidden="true" focusable="false">
    <circle cx="8" cy="8" r="5.5" fill="none" stroke="currentColor" strokeOpacity=".25" strokeWidth="1.8" />
    <path d="M8 2.5a5.5 5.5 0 0 1 5.5 5.5" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" />
  </svg>
);
