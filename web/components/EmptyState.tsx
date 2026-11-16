/** The Library's empty tab: a short title, one line on what goes there, and the way to add it. */
export function EmptyState({ title, children, action }: { title: string; children: React.ReactNode; action?: React.ReactNode }) {
  return (
    <div className="au-wrap au-empty-wrap">
      <div className="au-empty">
        <span className="au-empty-art" aria-hidden="true">
          <i />
          <i />
          <i />
        </span>
        <h2>{title}</h2>
        <p>{children}</p>
        {action}
      </div>
    </div>
  );
}
