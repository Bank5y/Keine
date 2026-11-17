import { useSyncExternalStore } from 'react';

// The width at which Keine turns into a phone app: a top bar with search, a
// bottom bar for the main pages. It matches the 760px breakpoint in styles.css.
const narrowQuery = window.matchMedia('(max-width: 760px)');

export function useNarrow() {
  return useSyncExternalStore((l) => { narrowQuery.addEventListener('change', l); return () => narrowQuery.removeEventListener('change', l); }, () => narrowQuery.matches);
}
