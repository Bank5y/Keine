import { useCallback, useEffect, useReducer, useRef, useState, type ComponentProps } from 'react';
import { createPortal } from 'react-dom';
import { ChevronDown, RotateCcw, Sparkles, X } from 'lucide-react';
import type { Components, ExtraProps } from 'react-markdown';
import { CodeBlock } from '@/components/code-block';
import { LazyMarkdown } from '@/components/lazy-markdown';
import { Mermaid } from '@/components/mermaid';
import { Button } from '@/components/ui/button';
import { Dialog } from '@/components/ui/dialog';
import { analyze, hasContent, initial, step, type AnalysisState, type Part, type Parts } from '@/lib/analysis';
import type { Quote } from '@/lib/annotations';
import { progressLines, summary, type ToolActivity } from '@/lib/progress';
import { cn } from '@/lib/utils';

const WIDTH = 440;

// where the popover opens: under the selection, level with its left edge
// (left in the viewport, bottom in the document)
export interface Anchor { left: number; bottom: number }

// a node of the rendered Markdown, as far as its text goes
type TextNode = { type: string; value?: string; children?: TextNode[] };

// textOf is the text of a node, as written.
const textOf = (n: TextNode | undefined): string =>
  !n ? '' : n.type === 'text' ? n.value ?? '' : (n.children ?? []).map(textOf).join('');

// a ```mermaid block draws once the answer is complete; while it streams,
// and if it cannot be drawn, it stays a code block
function diagrams(draw: boolean): Components {
  return {
    pre: (props: ComponentProps<'pre'> & ExtraProps) => {
      const code = props.node?.children.find((c) => c.type === 'element' && c.tagName === 'code');
      const cls = code && code.type === 'element' ? code.properties.className : undefined;
      const isMermaid = Array.isArray(cls) && cls.includes('language-mermaid');
      if (!isMermaid || !draw) return <CodeBlock {...props} />;
      return <Mermaid code={textOf(code as TextNode).replace(/\n$/, '')} fallback={<CodeBlock {...props} />} />;
    },
  };
}
const streamingParts = diagrams(false);
const finishedParts = diagrams(true);

// AnalysisPopover asks the assistant to analyze a selection and shows the
// answer as it streams: loading, streaming, done, or an error to retry.
// Closing it (Esc, a press outside, ×) asks first once it shows some of an
// answer, since an analysis not pinned is thrown away.
export function AnalysisPopover({ courseId, lessonId, quote, at, onClose }: {
  courseId: number; lessonId: number; quote: Quote; at: Anchor; onClose: () => void;
}) {
  const [state, dispatch] = useReducer(step, initial);
  const [attempt, setAttempt] = useState(0);
  const [asking, setAsking] = useState(false);
  const box = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const ctl = new AbortController();
    void analyze(courseId, lessonId, quote, dispatch, ctl.signal);
    return () => ctl.abort();
  }, [courseId, lessonId, quote, attempt]);

  const stateRef = useRef<AnalysisState>(state);
  stateRef.current = state;
  const requestClose = useCallback(() => {
    if (hasContent(stateRef.current)) setAsking(true);
    else onClose();
  }, [onClose]);

  useEffect(() => {
    if (asking) return undefined;
    const onDown = (e: PointerEvent) => { if (!box.current?.contains(e.target as Node)) requestClose(); };
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') requestClose(); };
    document.addEventListener('pointerdown', onDown);
    document.addEventListener('keydown', onKey);
    return () => { document.removeEventListener('pointerdown', onDown); document.removeEventListener('keydown', onKey); };
  }, [asking, requestClose]);

  const retry = () => { dispatch({ type: 'start' }); setAttempt((n) => n + 1); };
  const left = Math.max(16, Math.min(at.left - 3, innerWidth - WIDTH - 16) + scrollX);

  return (
    <>
      {createPortal(
        <div ref={box} className="analysis" role="dialog" aria-label="分析" style={{ left, top: at.bottom + 12 }}>
          <div className="analysis-head">
            <Sparkles className="analysis-mark" />
            <span className="analysis-kind">分析</span>
            <span className="analysis-quote" title={quote.quote}>「{quote.quote}」</span>
            <button type="button" className="analysis-close" aria-label="关闭" onClick={requestClose}><X /></button>
          </div>
          <Body state={state} />
          <Foot state={state} onRetry={retry} />
        </div>,
        document.body,
      )}
      <Dialog open={asking} onOpenChange={(v) => { if (!v) setAsking(false); }} title="丢弃这次分析？">
        <p>分析结果尚未固定，关闭后将丢弃。</p>
        <div className="head-actions">
          <Button onClick={() => setAsking(false)}>继续查看</Button>
          <Button variant="default" onClick={() => { setAsking(false); onClose(); }}>丢弃</Button>
        </div>
      </Dialog>
    </>
  );
}

// the section text is being written into, which shows the caret
function writing(state: AnalysisState): Part | null {
  if (state.status !== 'streaming') return null;
  const p = state.parts;
  return p.body ? 'body' : p.intro ? 'intro' : 'title';
}

// Progress lists the run's lookups, a running one with a pulsing dot and a
// failed one marked.
function Progress({ activities }: { activities: ToolActivity[] }) {
  return (
    <ul className="analysis-progress">
      {progressLines(activities).map((l, i) => (
        <li key={i} className={cn(l.running && 'running', l.failed && 'failed')}>
          <span className="analysis-dot" aria-hidden />{l.text}{l.failed ? <span className="analysis-failed">{l.note ?? '失败'}</span> : null}
        </li>
      ))}
    </ul>
  );
}

// Lookups fold into one line once the answer shows; it opens to the list.
function Lookups({ activities }: { activities: ToolActivity[] }) {
  const [open, setOpen] = useState(false);
  const line = summary(activities);
  if (!line) return null;
  return (
    <div className="analysis-lookups">
      <button type="button" className={cn('analysis-lookups-toggle', open && 'open')} aria-expanded={open} onClick={() => setOpen((v) => !v)}>
        {line}<ChevronDown />
      </button>
      {open ? <Progress activities={activities} /> : null}
    </div>
  );
}

function Body({ state }: { state: AnalysisState }) {
  if (state.status === 'loading') {
    const looking = state.activities.length > 0;
    return (
      <div className="analysis-body" aria-busy="true">
        {looking ? <Progress activities={state.activities} /> : null}
        {state.thinking
          ? <p className="analysis-wait thinking"><span className="analysis-dot" aria-hidden />正在思考…</p>
          : looking ? null : <p className="analysis-wait">正在结合本讲上下文分析…</p>}
        <div className="analysis-skeleton">
          <span style={{ width: 170, height: 16 }} />
          <span style={{ width: '100%' }} /><span style={{ width: '94%' }} /><span style={{ width: '78%' }} />
          <span style={{ width: 120, marginTop: 14 }} /><span style={{ width: '97%' }} />
        </div>
      </div>
    );
  }
  const p: Parts = state.parts;
  if (state.status === 'error' && !hasContent(state)) return null;
  const at = writing(state);
  return (
    <div className="analysis-body" aria-live="polite" aria-busy={state.status === 'streaming'}>
      <Lookups activities={state.activities} />
      {p.title ? <h3 className={cn('analysis-title', at === 'title' && 'caret')}>{p.title}</h3> : null}
      {p.intro ? <p className={cn('analysis-intro', at === 'intro' && 'caret')}>{p.intro}</p> : null}
      {p.body ? (
        <>
          <div className="analysis-label">含义</div>
          <div className={cn('analysis-meaning', at === 'body' && 'caret')}>
            <LazyMarkdown text={p.body} components={state.status === 'done' ? finishedParts : streamingParts} />
          </div>
        </>
      ) : null}
    </div>
  );
}

function Foot({ state, onRetry }: { state: AnalysisState; onRetry: () => void }) {
  if (state.status === 'loading') return null;
  if (state.status === 'error') {
    return (
      <>
        <div className={cn('analysis-error', hasContent(state) && 'after-content')} role="alert">
          <div className="analysis-error-title">分析没能完成</div>
          <div className="analysis-error-text">{state.message}</div>
        </div>
        <div className="analysis-foot end">
          <button type="button" className="analysis-retry" onClick={onRetry}><RotateCcw />重试</button>
        </div>
      </>
    );
  }
  return (
    <>
      {state.status === 'done' && state.noWeb ? <p className="analysis-no-web" role="status">这次没有拿到网络结果</p> : null}
      <div className="analysis-foot">
        {state.status === 'streaming' ? <><span>生成中…</span><span>Esc 关闭</span></> : <span>AI 生成，仅供参考</span>}
      </div>
    </>
  );
}
