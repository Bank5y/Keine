import { useSyncExternalStore } from 'react';

// What a narrow screen's top bar is searching on the page it scopes (see
// page-search.ts), shared by the bar that edits it and the page that
// filters by it: null while it is not searching, else the words typed
// (maybe none yet). The library keeps its words in the address (q); for it
// this only says the bar is searching. focusLibrary asks the library's bar
// to take the keyboard once it shows (a search started on another page).

let query: string | null = null;
let focusLibrary = false;
const listeners = new Set<() => void>();
const emit = () => listeners.forEach((l) => l());
const subscribe = (l: () => void) => { listeners.add(l); return () => { listeners.delete(l); }; };

export function setTopSearch(q: string | null) {
  if (q === query) return;
  query = q;
  emit();
}

export const useTopSearch = () => useSyncExternalStore(subscribe, () => query);

// usePageSearch is the top bar's search as a page reads it, on a narrow
// screen (on): whether it is searching, and the words, trimmed.
export function usePageSearch(on: boolean) {
  const q = useTopSearch();
  return { searching: on && q != null, q: on ? (q ?? '').trim() : '' };
}

export function askLibraryFocus() { focusLibrary = true; }
// takeLibraryFocus says, once, whether the library's bar should take the keyboard.
export function takeLibraryFocus() { const on = focusLibrary; focusLibrary = false; return on; }
