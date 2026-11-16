import { useCallback, useMemo, useState } from 'react';
import { Check, WandSparkles } from 'lucide-react';
import { toast } from 'sonner';
import { api, ApiError } from '@/lib/api';
import type { Quote } from '@/lib/annotations';

// The magic wand of annotation migration: the server proposes, by
// deterministic fuzzy matching, a place in the new text for each annotation
// awaiting migration. A suggestion is a quote of the new text exactly as a
// selection there would give it, so accepting one is placing the annotation
// on it like any other migration.
export interface Suggestion extends Quote {
  annotationId: number;
  range: { start: number; end: number };
  // 0 to 1: how alike the place's text and surroundings are to the quote's
  confidence: number;
}

// the placement a suggestion stands for, as place takes it
export const placementOf = ({ quote, prefix, suffix }: Suggestion): Quote => ({ quote, prefix, suffix });

export function useWand(courseId: number, lessonId: number) {
  // null until the wand has been waved
  const [list, setList] = useState<Suggestion[] | null>(null);
  const [busy, setBusy] = useState(false);
  const run = useCallback(async () => {
    setBusy(true);
    try {
      const r = await api<{ suggestions: Suggestion[] }>('GET', `/api/courses/${courseId}/lessons/${lessonId}/suggestions`);
      setList(r.suggestions);
    } catch (ex) {
      toast.error(ex instanceof ApiError ? ex.message : '没能找到建议位置');
    } finally { setBusy(false); }
  }, [courseId, lessonId]);
  const byId = useMemo(() => new Map((list ?? []).map((s) => [s.annotationId, s])), [list]);
  return { ran: list !== null, busy, run, byId };
}

export const confidenceLabel = (c: number) => `匹配度 ${Math.round(c * 100)}%`;

// WandBar waves the wand, then offers to accept every suggestion at once.
export function WandBar({ ran, busy, count, onRun, onAcceptAll }: {
  ran: boolean; busy: boolean; count: number; onRun: () => void; onAcceptAll: () => void;
}) {
  return (
    <div className="wand-bar" role="group" aria-label="魔棒">
      {ran ? (
        <>
          <span className="wand-sum">
            {count ? <>魔棒为 <b>{count}</b> 条标注找到了建议位置，逐条看过后接受，或者全部接受。</> : '魔棒没有更多建议了，剩下的标注请手动处理。'}
          </span>
          {count ? <button type="button" className="ann-btn primary" disabled={busy} onClick={onAcceptAll}><Check />全部接受</button> : null}
        </>
      ) : (
        <button type="button" className="ann-btn wand-run" disabled={busy} onClick={onRun}><WandSparkles />{busy ? '正在查找…' : '魔棒：找建议位置'}</button>
      )}
    </div>
  );
}

// WandSuggestion is one annotation's suggested place, inside its card; when
// the wand found none, it says so.
export function WandSuggestion({ suggestion, busy, onShow, onAccept }: {
  suggestion: Suggestion | undefined; busy: boolean; onShow: () => void; onAccept: () => void;
}) {
  if (!suggestion) return <div className="wand-none">魔棒没找到相近的文字，请手动处理</div>;
  return (
    <div className="wand-sug" data-suggestion={suggestion.annotationId}>
      <button type="button" className="wand-quote" onClick={onShow} title="在新正文里查看">
        <span className="wand-label"><WandSparkles />建议</span>
        <span className="wand-text">{suggestion.quote}</span>
      </button>
      <div className="wand-foot">
        <span className="wand-conf">{confidenceLabel(suggestion.confidence)}</span>
        <button type="button" className="ann-btn primary" disabled={busy} onClick={onAccept}><Check />接受</button>
      </div>
    </div>
  );
}
