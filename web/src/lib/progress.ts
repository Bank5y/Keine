// The progress of a run as the analysis popover shows it (ADR 0008): the
// server sends each tool call as an activity with its kind, target and
// status; the words are made here.

export interface ToolActivity {
  id: string;
  kind: string;
  target?: string;
  status: 'running' | 'done' | 'failed';
  reason?: string;
  // a failed web call ran on the reader's own key
  ownKey?: boolean;
}

// track records an activity: a new call is added at the end, a known one
// takes its latest status.
export function track(list: ToolActivity[], a: ToolActivity): ToolActivity[] {
  const i = list.findIndex((x) => x.id === a.id);
  if (i < 0) return [...list, a];
  const next = list.slice();
  next[i] = a;
  return next;
}

// note says why a web call failed, in place of the bare failure mark
export interface ProgressLine { text: string; running: boolean; failed: boolean; note?: string }

// the words of each kind: running, done, failed (with the target), and
// running for several at once
const words: Record<string, { running: string; done: string; failed: string; several: string }> = {
  read_lesson: { running: '正在查阅「%」', done: '查阅了「%」', failed: '没能查阅「%」', several: '正在查阅 # 处' },
  list_sections: { running: '正在查看「%」的目录', done: '查看了「%」的目录', failed: '没能查看「%」的目录', several: '正在查看 # 讲的目录' },
  search_course: { running: '正在课程里查找「%」', done: '在课程里查找了「%」', failed: '没能在课程里查找「%」', several: '正在课程里查找 # 项' },
  web_search: { running: '正在搜索「%」', done: '搜索了「%」', failed: '没能搜索「%」', several: '正在搜索 # 项' },
  web_fetch: { running: '正在读「%」', done: '读了「%」', failed: '没能读「%」', several: '正在读 # 个网页' },
};

// the failures of a web call that are the search service's, not the
// page's (ADR 0008): on the site's key the reader only hears that web
// search is unavailable; on their own key, why
const serviceReasons = new Set(['quota_exhausted', 'auth', 'unavailable', 'timeout', 'rate_limited', 'internal']);
const ownKeyNotes: Record<string, string> = { quota_exhausted: '你的搜索 key 余额不足', auth: '你的搜索 key 无效' };

function failureNote(a: ToolActivity): string | undefined {
  if (a.kind !== 'web_search' && a.kind !== 'web_fetch') return undefined;
  if (!serviceReasons.has(a.reason ?? '')) return undefined;
  return (a.ownKey && ownKeyNotes[a.reason ?? '']) || '联网搜索暂不可用';
}
const general = { running: '正在查阅「%」', done: '查阅了「%」', failed: '没能查阅「%」', several: '正在查阅 # 项' };
const wordsOf = (kind: string) => words[kind] ?? general;

// progressLines is the list of what the run looked up: one line per call,
// except that calls of a kind running at once share one.
export function progressLines(list: ToolActivity[]): ProgressLine[] {
  const running = new Map<string, number>();
  for (const a of list) if (a.status === 'running') running.set(a.kind, (running.get(a.kind) ?? 0) + 1);
  const merged = new Set<string>();
  const out: ProgressLine[] = [];
  for (const a of list) {
    const w = wordsOf(a.kind);
    const n = running.get(a.kind) ?? 0;
    if (a.status === 'running' && n > 1) {
      if (merged.has(a.kind)) continue;
      merged.add(a.kind);
      out.push({ text: w.several.replace('#', String(n)), running: true, failed: false });
      continue;
    }
    const note = a.status === 'failed' ? failureNote(a) : undefined;
    out.push({ text: w[a.status].replace('%', a.target ?? ''), running: a.status === 'running', failed: a.status === 'failed', ...(note ? { note } : {}) });
  }
  return out;
}

// summary is the one line the progress folds into once the answer shows:
// how many lessons were read, how many searches made, how many failed.
export function summary(list: ToolActivity[]): string {
  const lessons = new Set<string>();
  let searches = 0;
  let pages = 0;
  let failed = 0;
  for (const a of list) {
    if (a.status === 'failed') { failed++; continue; }
    if (a.status !== 'done') continue;
    // a read's target is the lesson's title, then the heading's after ' › '
    if (a.kind === 'read_lesson' || a.kind === 'list_sections') lessons.add((a.target ?? '').split(' › ')[0]);
    else if (a.kind === 'search_course' || a.kind === 'web_search') searches++;
    else if (a.kind === 'web_fetch') pages++;
  }
  const parts: string[] = [];
  if (lessons.size) parts.push(`查阅了 ${lessons.size} 讲`);
  if (searches) parts.push(`搜索 ${searches} 次`);
  if (pages) parts.push(`读了 ${pages} 个网页`);
  if (failed) parts.push(`${failed} 项失败`);
  return parts.join(' · ');
}
