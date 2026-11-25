// A reader's appearance settings: the palette, light or dark, and the fonts
// for reading and for code. They live in this browser only (localStorage);
// index.html applies them before the first paint, so keep the keys and the
// way they land on <html> in step with the script there.

export type Palette = 'graphite' | 'mokou';
export type Mode = 'system' | 'light' | 'dark';
export type FontRole = 'read' | 'code';

export const PALETTES: { id: Palette; label: string; paper: string; accent: string }[] = [
  { id: 'graphite', label: '石墨', paper: '#f0f0f0', accent: '#476666' },
  { id: 'mokou', label: '妹红', paper: '#f6f2eb', accent: '#b23a2a' },
];

export const MODES: { id: Mode; label: string }[] = [
  { id: 'system', label: '跟随系统' },
  { id: 'light', label: '浅色' },
  { id: 'dark', label: '深色' },
];

// Presets are font stacks that fall back to fonts readers already have; the
// stacks themselves are in styles.css under :root[data-font-<role>="<id>"].
export const PRESETS: Record<FontRole, { id: string; label: string }[]> = {
  read: [
    { id: 'default', label: '默认' },
    { id: 'song', label: '宋体' },
    { id: 'hei', label: '黑体' },
    { id: 'kai', label: '楷体' },
  ],
  code: [
    { id: 'default', label: '默认' },
    { id: 'jetbrains', label: 'JetBrains Mono' },
    { id: 'cascadia', label: 'Cascadia Code' },
    { id: 'system', label: '系统等宽' },
  ],
};

// A font choice is a preset, or a font family on this computer.
export type FontChoice = { preset: string } | { family: string };

export const KEYS = { mode: 'keine.theme', palette: 'keine.palette', read: 'keine.font.read', code: 'keine.font.code' } as const;
const FAMILY = 'family:';
export const CHANGED = 'keine:appearance';

export function encodeFont(c: FontChoice): string | null {
  if ('family' in c) return c.family.trim() ? FAMILY + c.family.trim() : null;
  return c.preset === 'default' ? null : c.preset;
}

export function decodeFont(role: FontRole, v: string | null): FontChoice {
  if (v?.startsWith(FAMILY) && v.length > FAMILY.length) return { family: v.slice(FAMILY.length) };
  if (v && PRESETS[role].some((p) => p.id === v)) return { preset: v };
  return { preset: 'default' };
}

// familyStack puts a reader's own font first, falling back to the default
// stack for whatever it lacks (or when it is not installed at all).
export function familyStack(role: FontRole, family: string): string {
  return `"${family.replace(/["\\]/g, '')}", var(--${role === 'read' ? 'serif' : 'mono'})`;
}

export interface Appearance { palette: Palette; mode: Mode; read: FontChoice; code: FontChoice }

export function readAppearance(get: (k: string) => string | null): Appearance {
  const m = get(KEYS.mode);
  return {
    palette: get(KEYS.palette) === 'mokou' ? 'mokou' : 'graphite',
    mode: m === 'light' || m === 'dark' ? m : 'system',
    read: decodeFont('read', get(KEYS.read)),
    code: decodeFont('code', get(KEYS.code)),
  };
}

function store(k: string, v: string | null) {
  try {
    if (v === null) localStorage.removeItem(k);
    else localStorage.setItem(k, v);
  } catch { /* private mode: the choice lasts this page only */ }
}

function changed() { window.dispatchEvent(new Event(CHANGED)); }

export function loadAppearance(): Appearance {
  return readAppearance((k) => { try { return localStorage.getItem(k); } catch { return null; } });
}

export function setPalette(p: Palette) {
  const root = document.documentElement;
  if (p === 'graphite') delete root.dataset.palette;
  else root.dataset.palette = p;
  store(KEYS.palette, p === 'graphite' ? null : p);
  changed();
}

export function setMode(m: Mode) {
  const root = document.documentElement;
  if (m === 'system') delete root.dataset.theme;
  else root.dataset.theme = m;
  store(KEYS.mode, m === 'system' ? null : m);
  changed();
}

export function setFont(role: FontRole, c: FontChoice) {
  const root = document.documentElement;
  const v = encodeFont(c);
  const attr = role === 'read' ? 'fontRead' : 'fontCode';
  root.style.removeProperty(`--${role}-custom`);
  delete root.dataset[attr];
  if (v?.startsWith(FAMILY)) root.style.setProperty(`--${role}-custom`, familyStack(role, v.slice(FAMILY.length)));
  else if (v) root.dataset[attr] = v;
  store(KEYS[role], v);
  changed();
}
