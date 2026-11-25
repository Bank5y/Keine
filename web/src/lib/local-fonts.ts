// Finding the fonts on this computer, in the browser only: nothing here is
// sent anywhere. Chrome and Edge list every installed font through the Local
// Font Access API (it asks for permission once); other browsers cannot list
// fonts, so we probe a list of common ones by measuring text.

// Common fonts to probe for where the browser cannot list them.
export const CANDIDATES = [
  // Chinese
  'PingFang SC', 'Hiragino Sans GB', 'Songti SC', 'Kaiti SC', 'STSong', 'STKaiti', 'STHeiti', 'STFangsong',
  'Microsoft YaHei', 'SimSun', 'SimHei', 'KaiTi', 'FangSong', 'DengXian',
  'Noto Sans SC', 'Noto Serif SC', 'Noto Sans CJK SC', 'Noto Serif CJK SC', 'Source Han Sans SC', 'Source Han Serif SC',
  'WenQuanYi Micro Hei', 'WenQuanYi Zen Hei', 'LXGW WenKai', 'LXGW WenKai Screen', 'Sarasa Gothic SC', 'HarmonyOS Sans SC', 'MiSans',
  // Latin
  'Georgia', 'Times New Roman', 'Palatino', 'Charter', 'Iowan Old Style', 'Source Serif 4', 'Merriweather', 'Literata',
  'Helvetica Neue', 'Arial', 'Segoe UI', 'Inter', 'Roboto', 'Open Sans', 'Lato', 'Ubuntu', 'Cantarell', 'DejaVu Sans', 'DejaVu Serif',
  'Noto Sans', 'Noto Serif', 'Liberation Sans', 'Liberation Serif', 'Adwaita Sans',
  // monospace
  'JetBrains Mono', 'Fira Code', 'Cascadia Code', 'Cascadia Mono', 'Source Code Pro', 'IBM Plex Mono', 'Hack',
  'SF Mono', 'Menlo', 'Monaco', 'Consolas', 'Courier New', 'DejaVu Sans Mono', 'Ubuntu Mono', 'Iosevka', 'Maple Mono',
  'Noto Sans Mono', 'Liberation Mono', 'Adwaita Mono',
];

const SAMPLE = 'mmmmmmmmmmlliWW@#永和九年岁在癸丑';
const BASES = ['monospace', 'serif', 'sans-serif'];

function quote(family: string) { return `"${family.replace(/["\\]/g, '')}"`; }

let ctx: CanvasRenderingContext2D | null | undefined;
function measure(font: string, text: string): number {
  if (ctx === undefined) ctx = document.createElement('canvas').getContext('2d');
  if (!ctx) return 0;
  ctx.font = font;
  return ctx.measureText(text).width;
}

// installed tells whether this computer has a font: a font that is missing
// falls back to the generic family, so the text measures the same as that.
export function installed(family: string): boolean {
  if (!family.trim()) return false;
  return BASES.some((b) => measure(`32px ${quote(family)}, ${b}`, SAMPLE) !== measure(`32px ${b}`, SAMPLE));
}

// monospace tells whether every letter of an installed font is as wide.
export function monospace(family: string): boolean {
  const f = `32px ${quote(family)}, serif`;
  return Math.abs(measure(f, 'iiiiiiiiii') - measure(f, 'WWWWWWWWWW')) < 0.5;
}

// families dedupes font family names and sorts them for a list.
export function families(names: Iterable<string>): string[] {
  const seen = new Map<string, string>();
  for (const n of names) {
    const t = n.trim();
    if (t && !seen.has(t.toLowerCase())) seen.set(t.toLowerCase(), t);
  }
  return [...seen.values()].sort((a, b) => a.localeCompare(b, 'zh-CN'));
}

// splitMonospace puts the monospace fonts first, for choosing a code font.
export function splitMonospace(names: string[], isMono: (f: string) => boolean): { mono: string[]; other: string[] } {
  const mono: string[] = [];
  const other: string[] = [];
  for (const n of names) (isMono(n) ? mono : other).push(n);
  return { mono, other };
}

type QueryLocalFonts = () => Promise<{ family: string }[]>;

// detectFonts lists the font families on this computer.
export async function detectFonts(): Promise<string[]> {
  const q = (window as unknown as { queryLocalFonts?: QueryLocalFonts }).queryLocalFonts;
  if (q) {
    try {
      const listed = families((await q.call(window)).map((f) => f.family));
      if (listed.length) return listed;
      // an empty list is what a refused permission can look like too
    } catch {
      // permission refused, or not allowed here: probe instead
    }
  }
  return families(CANDIDATES.filter(installed));
}
