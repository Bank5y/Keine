// Credits, plans and usage (#130, ADR 0010) as the pages show them.

// Account is a reader's credits today: daily and remaining are null on an
// unlimited plan; remaining goes below zero when a run overdraws.
export interface Account { planId: number; plan: string; daily: number | null; used: number; remaining: number | null }

export interface Plan { id: number; name: string; daily: number | null; default: boolean; builtin: boolean; users: number }

// Totals is the usage of some runs added up; estimated counts the runs
// worked out locally.
export interface Totals {
  runs: number; input: number; cacheRead: number; cacheWrite: number; output: number; reasoning: number
  searches: number; credits: number; estimated: number
}
export interface Day extends Totals { date: string }
export interface ReaderUsage extends Totals { id: number; username: string; plan: string }
export interface Run {
  id: number; kind: string; model: string; input: number | null; cacheRead: number | null; cacheWrite: number | null
  output: number | null; reasoning: number | null; searches: number; credits: number; estimated: boolean; at: string
}

const fmt = new Intl.NumberFormat('en-US', { maximumFractionDigits: 2 });

// credits shows an amount to two places at most.
export const credits = (n: number) => fmt.format(n);

// quota shows a daily quota, 不限 for none.
export const quota = (n: number | null) => (n === null ? '不限' : credits(n));

// tokens shortens a token count: 412k, 1.31M.
export function tokens(n: number) {
  if (n >= 1_000_000) return `${+(n / 1_000_000).toFixed(2)}M`;
  if (n >= 1_000) return `${+(n / 1_000).toFixed(1)}k`;
  return String(n);
}

// the sum of a run's four kinds of tokens
export const allTokens = (t: Pick<Totals, 'input' | 'cacheRead' | 'cacheWrite' | 'output'>) => t.input + t.cacheRead + t.cacheWrite + t.output;

export type Preset = 'today' | '7d' | '30d';
export interface DateRange { from: string; to: string }

const day = (d: Date) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;

// rangeOf is the days a preset covers, today included.
export function rangeOf(p: Preset, today = new Date()): DateRange {
  const back = { today: 0, '7d': 6, '30d': 29 }[p];
  const from = new Date(today.getFullYear(), today.getMonth(), today.getDate() - back);
  return { from: day(from), to: day(today) };
}
