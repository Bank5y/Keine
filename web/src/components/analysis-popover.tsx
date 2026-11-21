import { useCallback, useEffect, useReducer, useRef, useState, type ComponentProps, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import { ChevronDown, ChevronUp, Pin, RotateCcw, Sparkles, X } from 'lucide-react';
import { toast } from 'sonner';
import type { Components, ExtraProps } from 'react-markdown';
import { CodeBlock } from '@/components/code-block';
import { LazyMarkdown } from '@/components/lazy-markdown';
import { Mermaid } from '@/components/mermaid';
import { Button } from '@/components/ui/button';
import { Dialog } from '@/components/ui/dialog';
import { analyze, hasContent, initial, pinRefusals, step, type AnalysisState, type LessonLength, type Length, type Part, type Parts, type Refusal } from '@/lib/analysis';
import { ApiError } from '@/lib/api';
import { pinAnalysis, type Exploration } from '@/lib/explorations';
import type { Quote } from '@/lib/annotations';
import { readNotice, useUser } from '@/lib/session';
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

// Pinning is where pinning a finished analysis stands: refused, the
// content stays and the popover says why.
type Pinning = { status: 'idle' } | { status: 'pinning' } | ({ status: 'refused'; message: string } & Refusal);

// AnalysisPopover asks the assistant to analyze a selection and shows the
// answer as it streams: loading, streaming, done, or an error to retry.
// A finished answer can be pinned (固定探索), which hands the exploration
// to onPinned. Closing it (Esc, a press outside, ×) asks first once it
// shows some of an answer, since an analysis not pinned is thrown away.
export function AnalysisPopover({ courseId, lessonId, quote, at, onClose, onPinned }: {
  courseId: number; lessonId: number; quote: Quote; at: Anchor; onClose: () => void; onPinned: (e: Exploration) => void;
}) {
  const user = useUser();
  const [state, dispatch] = useReducer(step, initial);
  const [attempt, setAttempt] = useState(0);
  const [asking, setAsking] = useState(false);
  // a reader who has not read the notice reads it first, and starts the
  // run with 分析 (#132)
  const [started, setStarted] = useState(!!user?.aiNoticeRead);
  const [noticeOpen, setNoticeOpen] = useState(false);
  // how much of a long lesson to send, once the reader chose (#131)
  const [length, setLength] = useState<Length>();
  const [choice, setChoice] = useState<Length>('related');
  const box = useRef<HTMLDivElement>(null);
  const [pinning, setPinning] = useState<Pinning>({ status: 'idle' });

  useEffect(() => {
    if (!started) return undefined;
    const ctl = new AbortController();
    void analyze(courseId, lessonId, quote, length, dispatch, ctl.signal);
    return () => ctl.abort();
  }, [courseId, lessonId, quote, attempt, started, length]);

  const start = () => {
    setStarted(true);
    // the run goes ahead whether or not the mark is saved; unsaved, the
    // notice shows again next time
    readNotice().catch(() => {});
  };

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

  const retry = () => { dispatch({ type: 'start' }); setPinning({ status: 'idle' }); setAttempt((n) => n + 1); };

  const pin = (analysisId: string) => {
    setPinning({ status: 'pinning' });
    pinAnalysis(courseId, lessonId, analysisId).then(onPinned).catch((ex: unknown) => {
      const refusal = ex instanceof ApiError ? pinRefusals[ex.code] : undefined;
      if (refusal) { setPinning({ status: 'refused', ...refusal, message: (ex as ApiError).message }); return; }
      toast.error(ex instanceof ApiError ? ex.message : '没能固定，请重试。');
      setPinning({ status: 'idle' });
    });
  };
  const left = Math.max(16, Math.min(at.left - 3, innerWidth - WIDTH - 16) + scrollX);

  const credit = <Credit provider={user?.aiProvider} open={noticeOpen} onToggle={() => setNoticeOpen((v) => !v)} />;
  let main: ReactNode, foot: ReactNode;
  if (!started) {
    main = (
      <div className="analysis-body">
        <p className="analysis-lead">分析会把下面这些内容发给 {user?.aiProvider || '模型服务'}，由它生成解读。</p>
        <Notice search={user?.aiSearch} className="analysis-notice" />
      </div>
    );
    foot = (
      <div className="analysis-foot">
        <span>AI 生成 · {user?.aiProvider || '模型服务'}</span>
        <Button size="sm" variant="default" onClick={start}><Sparkles />分析</Button>
      </div>
    );
  } else if (state.status === 'choose') {
    main = <Choose length={state.length} choice={choice} onChange={setChoice} />;
    foot = (
      <div className="analysis-foot">
        {credit}
        <Button size="sm" variant="default" onClick={() => { dispatch({ type: 'start' }); setLength(choice); setAttempt((n) => n + 1); }}><Sparkles />分析</Button>
      </div>
    );
  } else {
    main = <Body state={state} />;
    foot = <Foot state={state} pinning={pinning} onRetry={retry} onPin={pin}>{credit}</Foot>;
  }

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
          {main}
          {started && noticeOpen ? (
            <div className="analysis-notice-panel" id="analysis-notice">
              <div className="analysis-notice-title">发给第三方服务的内容</div>
              <Notice search={user?.aiSearch} />
            </div>
          ) : null}
          {foot}
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

function Body({ state }: { state: Exclude<AnalysisState, { status: 'choose' }> }) {
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

// Choose asks how much of a long lesson to send (#131): the sections
// around the selection by default, or the whole lesson while it fits.
function Choose({ length, choice, onChange }: { length: LessonLength; choice: Length; onChange: (l: Length) => void }) {
  const options: { id: Length; title: string; note: string; disabled?: boolean }[] = [
    { id: 'related', title: '只看相关小节', note: '选区所在小节、相邻小节和本讲小节目录' },
    length.full
      ? { id: 'full', title: '完整正文', note: '分析更完整，但更慢、更耗积分' }
      : { id: 'full', title: '完整正文', note: '超过了模型能读的长度，不能选', disabled: true },
  ];
  return (
    <div className="analysis-body">
      <p className="analysis-lead">这一讲比较长（约 {length.chars >= 10000 ? `${(length.chars / 10000).toFixed(1)} 万` : Math.round(length.chars / 100) * 100} 字）。选择这次分析要放入多少正文：</p>
      <div className="length-options" role="radiogroup" aria-label="正文长度">
        {options.map((o) => (
          <label key={o.id} className={cn('length-option', choice === o.id && 'checked', o.disabled && 'disabled')}>
            <input type="radio" name="length" value={o.id} checked={choice === o.id} disabled={o.disabled} onChange={() => onChange(o.id)} />
            <span className="length-option-title">{o.title}{o.id === 'related' ? <small>默认</small> : null}</span>
            <span className="length-option-note">{o.note}</span>
          </label>
        ))}
      </div>
    </div>
  );
}

// Notice says what goes to third-party services and what does not (#132);
// search is where searches go ('' or absent when the run has no web).
function Notice({ search, className }: { search?: string; className?: string }) {
  return (
    <dl className={cn('notice', className)}>
      <dt>会发送</dt>
      <dd>选中的原文、本讲全文、课程名称和目录、你的提问和探索文档{search ? `；联网搜索时，搜索词会发给 ${search}` : ''}。</dd>
      <dt>不会发送</dt>
      <dd>你的标注、笔记和其他探索。</dd>
      <dt>使用自带模型时</dt>
      <dd>以上内容发到你自己配置的服务地址。</dd>
    </dl>
  );
}

// Credit names the service that wrote the answer; it opens the notice.
function Credit({ provider, open, onToggle }: { provider?: string; open: boolean; onToggle: () => void }) {
  return (
    <button type="button" className="analysis-credit" aria-expanded={open} aria-controls="analysis-notice" onClick={onToggle}>
      AI 生成 · {provider || '模型服务'}{open ? <ChevronUp /> : <ChevronDown />}
    </button>
  );
}

// Foot ends the popover: credit (the service that wrote the answer) on the
// left, what can be done on the right: 固定探索 once the answer is done,
// 重试 after an error.
function Foot({ state, pinning, onRetry, onPin, children: credit }: {
  state: Exclude<AnalysisState, { status: 'choose' }>; pinning: Pinning; onRetry: () => void; onPin: (analysisId: string) => void; children: ReactNode;
}) {
  const failure = state.status === 'error'
    ? { message: state.message, ...(state.refusal ?? { title: '分析没能完成', retry: true }) }
    : pinning.status === 'refused' ? pinning : null;
  if (failure) {
    const { title, message, retry } = failure;
    return (
      <>
        <div className={cn('analysis-error', hasContent(state) && 'after-content')} role="alert">
          <div className="analysis-error-title">{title}</div>
          <div className="analysis-error-text">{message}</div>
        </div>
        <div className="analysis-foot">
          {credit}
          {retry ? <button type="button" className="analysis-retry" onClick={onRetry}><RotateCcw />重试</button> : null}
        </div>
      </>
    );
  }
  return (
    <>
      {state.status === 'done' && state.noWeb ? <p className="analysis-no-web" role="status">这次没有拿到网络结果</p> : null}
      <div className="analysis-foot">
        {credit}
        {state.status === 'streaming' ? <span>生成中… Esc 关闭</span> : null}
        {state.status === 'done' && state.analysisId ? (
          <Button size="sm" variant="default" disabled={pinning.status === 'pinning'} onClick={() => onPin(state.analysisId!)}>
            <Pin />{pinning.status === 'pinning' ? '固定中…' : '固定探索'}
          </Button>
        ) : null}
      </div>
    </>
  );
}
