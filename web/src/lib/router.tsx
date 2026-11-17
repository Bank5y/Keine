import { useSyncExternalStore, type ComponentProps, type MouseEvent } from 'react';

// A tiny history router: the path is the single source of truth, so every
// page (including a lesson) can be opened or shared by URL.

const listeners = new Set<() => void>();
const emit = () => listeners.forEach((l) => l());
window.addEventListener('popstate', emit);

// keepScroll: the page stays where it is, for a change within the page
// (switching the tab of a tabbed page).
export function navigate(to: string, opts: { replace?: boolean; keepScroll?: boolean } = {}) {
  if (to === location.pathname + location.search) return;
  if (opts.replace) history.replaceState(null, '', to);
  else history.pushState(null, '', to);
  if (!opts.keepScroll) window.scrollTo(0, 0);
  emit();
}

export function usePath() {
  return useSyncExternalStore((l) => { listeners.add(l); return () => listeners.delete(l); }, () => location.pathname);
}

// match returns the named segments of a pattern like "/courses/:id", or null.
export function match(pattern: string, path: string): Record<string, string> | null {
  const p = pattern.split('/').filter(Boolean);
  const s = path.split('/').filter(Boolean);
  if (p.length !== s.length) return null;
  const out: Record<string, string> = {};
  for (let i = 0; i < p.length; i++) {
    if (p[i].startsWith(':')) out[p[i].slice(1)] = decodeURIComponent(s[i]);
    else if (p[i] !== s[i]) return null;
  }
  return out;
}

export function Link({ href, onClick, keepScroll, ...rest }: ComponentProps<'a'> & { href: string; keepScroll?: boolean }) {
  const click = (e: MouseEvent<HTMLAnchorElement>) => {
    onClick?.(e);
    if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
    e.preventDefault();
    navigate(href, { keepScroll });
  };
  return <a href={href} onClick={click} {...rest} />;
}
