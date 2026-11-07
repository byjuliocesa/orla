import { useEffect, useState } from 'react';
import { GitHubMark, Star } from './Icons';

const REPO = 'byjuliocesa/orla';
const VERSION = import.meta.env.VITE_ORLA_VERSION ?? 'dev';
const CACHE = 'orla-stars';

// GitHub allows 60 unauthenticated requests an hour per address, so the count is kept for the tab's life.
async function stars(): Promise<number | null> {
  try {
    const cached = sessionStorage.getItem(CACHE);
    if (cached !== null) return Number(cached);
  } catch {}
  try {
    const res = await fetch(`https://api.github.com/repos/${REPO}`, { headers: { Accept: 'application/vnd.github+json' } });
    if (!res.ok) return null;
    const n = (await res.json()).stargazers_count;
    if (typeof n !== 'number') return null;
    try {
      sessionStorage.setItem(CACHE, String(n));
    } catch {}
    return n;
  } catch {
    return null;
  }
}

export function RepoBadge() {
  const [count, setCount] = useState<number | null>(null);
  useEffect(() => {
    stars().then(setCount);
  }, []);
  const starText = count === null ? '' : `, ${count} ${count === 1 ? 'star' : 'stars'}`;
  return (
    <a
      className="repo-badge"
      href={`https://github.com/${REPO}`}
      target="_blank"
      rel="noreferrer"
      aria-label={`Orla ${VERSION} on GitHub${starText}`}
    >
      <GitHubMark />
      <span className="repo-version">{VERSION}</span>
      {count !== null && (
        <span className="repo-stars">
          <Star />
          {count.toLocaleString('en')}
        </span>
      )}
    </a>
  );
}
