export default function NotFound() {
  return (
    <main className="login">
      <div className="empty" style={{ margin: 0 }}>
        <h2>Nothing here</h2>
        <p>This address is not part of Orla.</p>
        <a className="btn" href="/">
          Open Orla
        </a>
      </div>
    </main>
  );
}
