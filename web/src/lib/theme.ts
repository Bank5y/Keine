import { setMode } from '@/lib/appearance';

// toggleTheme flips between light and dark from whatever is showing now,
// the same setting as 明暗 on the account page.
export function toggleTheme() {
  const root = document.documentElement;
  const dark = root.dataset.theme ? root.dataset.theme === 'dark' : matchMedia('(prefers-color-scheme: dark)').matches;
  setMode(dark ? 'light' : 'dark');
}
