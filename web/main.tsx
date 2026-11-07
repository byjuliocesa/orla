import { StrictMode, useEffect, useState } from 'react';
import { createRoot } from 'react-dom/client';
import '@fontsource-variable/inter';
import '@fontsource-variable/jetbrains-mono';
import '@xterm/xterm/css/xterm.css';
import './styles/globals.css';
import Login from './pages/Login';
import NotFound from './pages/NotFound';
import Panel from './pages/Panel';

// One bundle for every page: the build copies index.html to login/index.html and 404.html, and
// the path picks the page. The login hands over to the panel without a reload (see Arrival).
function page(path: string) {
  if (path === '/' || path === '/index.html') return Panel;
  if (path === '/login' || path.startsWith('/login/')) return Login;
  return NotFound;
}

function App() {
  const [path, setPath] = useState(() => window.location.pathname);
  useEffect(() => {
    const read = () => setPath(window.location.pathname);
    addEventListener('popstate', read);
    return () => removeEventListener('popstate', read);
  }, []);
  const Page = page(path);
  return <Page />;
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
