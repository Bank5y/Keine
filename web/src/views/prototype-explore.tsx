// PROTOTYPE (throwaway, #74): how should analysis streaming and the
// exploration card's two-level zoom look and feel? Three variants on
// /prototype/explore?variant=A|B|C, all mock data, front end only:
//   A  逐字流式 + 卡片从边栏飞到中央（FLIP）
//   B  逐句淡入 + 卡片在边栏原地向左展开
//   C  三段骨架先占位、逐段填充 + 从中央弹性放大
//   D  选定方案：C 的分段骨架 + 新字由灰渐显（类 GPT）+ B 的原地展开
import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { Pin, RotateCcw, Sparkles, Telescope, Trash2, X, NotebookPen, Underline } from 'lucide-react';
import { useParam } from '@/lib/router';
import { PrototypeSwitcher } from '@/components/prototype-switcher';
import './prototype-explore.css';

type Variant = 'A' | 'B' | 'C' | 'D';
const NAMES: Record<Variant, string> = {
  A: '逐字流式 + 从边栏飞出',
  B: '逐句淡入 + 原地展开',
  C: '分段骨架 + 中央弹性放大',
  D: '选定：分段渐显 + 原地展开',
};

const MOCK = `# Agent（智能体）
能够自主规划步骤、调用工具，并根据执行结果调整下一步行动的大模型应用。

在这一讲里，Agent 指把用户目标拆成若干步骤、每一步交给模型或工具执行的系统。它与「把所有逻辑塞进一个提示词」的做法相对：后者指望模型一次想清楚，前者把控制流写在代码里，更容易调试和评估。

文中的 run 函数就是一个最小的 Agent：planner 负责规划，executor 逐步执行，出错时按约定重试。`;

// split splits a (maybe partial) analysis into its three parts.
function split(md: string) {
  const lines = md.split('\n');
  const title = lines[0]?.startsWith('# ') ? lines[0].slice(2) : '';
  const rest = lines.slice(1).join('\n').replace(/^\n+/, '');
  const [intro = '', ...body] = rest.split(/\n\n+/);
  return { title, intro, body: body.filter(Boolean) };
}

type Phase = 'idle' | 'bar' | 'loading' | 'streaming' | 'done' | 'error' | 'fixed';
type Zoom = 0 | 1 | 2;
type Rect = { left: number; top: number; width: number; height: number };

export function PrototypeExplore() {
  const param = useParam('variant');
  const v = (['A', 'B', 'C', 'D'].includes(param) ? param : 'D') as Variant;
  const [run, setRun] = useState(0); // remounts the page to start over
  const [fail, setFail] = useState(false);
  return (
    <div className="px-root">
      <Reader key={`${v}-${run}`} variant={v} fail={fail} />
      <PrototypeSwitcher variants={['A', 'B', 'C', 'D']} current={v} names={NAMES} extra={<>
        <button type="button" className="px-sw-btn" onClick={() => setRun((n) => n + 1)}>重来</button>
        <label className="px-sw-check"><input type="checkbox" checked={fail} onChange={(e) => setFail(e.target.checked)} />模拟出错</label>
      </>} />
    </div>
  );
}

function Reader({ variant, fail }: { variant: Variant; fail: boolean }) {
  const article = useRef<HTMLDivElement>(null);
  const [phase, setPhase] = useState<Phase>('idle');
  const [anchor, setAnchor] = useState<Rect | null>(null); // selection, page coords relative to article
  const [overlap, setOverlap] = useState(false);
  const [text, setText] = useState('');
  const [bounds, setBounds] = useState<number[]>([]); // where each streamed chunk ends
  const [confirm, setConfirm] = useState(false);
  const [cardTop, setCardTop] = useState<number | null>(null);
  const [zoom, setZoom] = useState<Zoom>(0);
  const [active, setActive] = useState(false);
  const range = useRef<Range | null>(null);
  const passage = useRef<HTMLSpanElement | null>(null);
  const timer = useRef<number[]>([]);
  const stop = () => { timer.current.forEach(clearTimeout); timer.current = []; };
  useEffect(() => stop, []);

  // selecting text shows the bar
  useEffect(() => {
    const up = () => {
      if (phase === 'loading' || phase === 'streaming' || phase === 'done' || phase === 'error' || zoom) return;
      const sel = window.getSelection();
      if (!sel || sel.isCollapsed || !article.current) return;
      const r = sel.getRangeAt(0);
      if (!article.current.querySelector('.px-text')?.contains(r.commonAncestorContainer)) return;
      const box = r.getBoundingClientRect(), base = article.current.getBoundingClientRect();
      range.current = r.cloneRange();
      setOverlap(!!passage.current && r.intersectsNode(passage.current));
      setAnchor({ left: box.left - base.left, top: box.top - base.top, width: box.width, height: box.height });
      setPhase((p) => (p === 'fixed' || p === 'idle' || p === 'bar' ? 'bar' : p));
    };
    document.addEventListener('mouseup', up);
    return () => document.removeEventListener('mouseup', up);
  });

  const analyse = () => {
    stop(); setText(''); setBounds([]); setPhase('loading');
    window.getSelection()?.removeAllRanges();
    timer.current.push(window.setTimeout(() => {
      if (fail) { setPhase('error'); return; }
      setPhase('streaming');
      // A streams a few characters at a time, B a sentence at a time,
      // C a few characters at a time into the pre-laid sections
      const pieces = variant === 'B' ? MOCK.split(/(?<=[。：\n])/) : MOCK.match(/[\s\S]{1,3}/g)!;
      let t = 0, acc = '';
      pieces.forEach((p, i) => {
        t += variant === 'B' ? 260 + Math.random() * 220 : 18 + Math.random() * 40;
        timer.current.push(window.setTimeout(() => {
          acc += p; const end = acc.length; setText(acc); setBounds((b) => [...b, end]);
          if (i === pieces.length - 1) setPhase('done');
        }, t));
      });
    }, 900));
  };

  const close = (force = false) => {
    if (!force && (phase === 'streaming' || phase === 'done') && text) { setConfirm(true); return; }
    stop(); setConfirm(false); setText('');
    setPhase(passage.current ? 'fixed' : 'idle');
  };

  const fix = () => {
    const r = range.current;
    if (!r || !article.current) return;
    const span = document.createElement('span');
    span.className = 'px-passage';
    try { r.surroundContents(span); } catch { span.appendChild(r.extractContents()); r.insertNode(span); }
    passage.current = span;
    span.addEventListener('click', () => { setActive(true); cardRef.current?.scrollIntoView({ block: 'nearest', behavior: 'smooth' }); });
    const base = article.current.getBoundingClientRect();
    setCardTop(span.getBoundingClientRect().top - base.top - 4);
    setPhase('fixed'); setActive(true); setText(MOCK);
  };

  useEffect(() => {
    const key = (e: KeyboardEvent) => { if (e.key !== 'Escape') return;
      if (zoom) { setZoom(zoom === 2 ? 0 : 0); return; }
      if (confirm) { setConfirm(false); return; }
      if (phase === 'loading' || phase === 'streaming' || phase === 'done' || phase === 'error') close();
      else if (phase === 'bar') setPhase(passage.current ? 'fixed' : 'idle');
    };
    window.addEventListener('keydown', key);
    return () => window.removeEventListener('keydown', key);
  });

  const cardRef = useRef<HTMLButtonElement>(null);
  const parts = split(text || MOCK);
  const popTop = anchor ? anchor.top + anchor.height + 8 : 0;

  return (
    <div className="px-page">
      <aside className="px-toc">
        <div className="px-back">← 返回课程</div>
        <div className="px-course">Agent 系统设计 40 讲</div>
        <div className="px-toc-item on">01｜用户输入规范化</div>
        <div className="px-toc-item">02｜三层意图识别</div>
        <div className="px-toc-item">03｜槽位抽取与追问</div>
      </aside>
      <div className="px-article" ref={article}>
        <div className="px-kicker">AGENT 系统设计 40 讲</div>
        <h1 className="px-h1">01｜用户输入规范化</h1>
        <div className="px-text">
          <p>Agent不是一个新概念，但在大模型出现之后，它<mark className="px-hl">第一次变得真正可用</mark>。这一讲我们从一个具体的问题出发，看看它在工程上到底意味着什么。</p>
          <p>很多团队在第一次落地时都会遇到同一个坑：把所有逻辑塞进一个提示词里，期待模型自己想清楚。结果是效果不稳定，出了问题也很难排查。</p>
          <p>更好的做法是把任务拆成几个职责清晰的步骤，<span className="px-ul">每一步只做一件事，并且有明确的输入和输出</span>。这样不仅更容易调试，也更容易评估每一步的质量。</p>
          <h2 className="px-h2">一个最小的例子</h2>
          <p>下面这段代码展示了一个最小的实现。注意我们没有引入任何框架，所有的控制流都是显式的：planner 负责规划，executor 逐步执行，出错时按约定重试。</p>
          <p>在后面几讲里，我们会分别展开规划、执行和记忆。它们都是 Agent 的组成部分，也都可以单独评估。</p>
        </div>
        <p className="px-hint">选中正文里的任意文字（比如开头的「Agent」），点浮条上的「分析」。</p>

        {phase === 'bar' && anchor ? (
          <div className="px-bar" style={{ left: anchor.left - 4, top: anchor.top - 46 }} onMouseUp={(e) => e.stopPropagation()}>
            <span className="px-bar-btn"><Underline />横线</span>
            <span className="px-bar-dim">荧光笔</span>
            {['#f2d34f', '#8fd19e', '#8fc0ee', '#f1a1bd'].map((c) => <i key={c} className="px-dot" style={{ background: c }} />)}
            <span className="px-sep" />
            <span className="px-bar-btn"><NotebookPen />写笔记</span>
            <span className="px-tip-wrap">
              <button type="button" className="px-bar-btn px-analyse" disabled={overlap} onClick={analyse}><Sparkles />分析</button>
              {overlap ? <span className="px-tip">这段文字已有探索</span> : null}
            </span>
          </div>
        ) : null}

        {anchor && (phase === 'loading' || phase === 'streaming' || phase === 'done' || phase === 'error') ? (
          <div className={`px-pop v-${variant}`} style={{ left: Math.max(0, anchor.left - 4), top: popTop }} onMouseUp={(e) => e.stopPropagation()}>
            <div className="px-pop-head">
              <Sparkles className="px-brand" /><b>分析</b><span className="px-quote">「{range.current?.toString().slice(0, 16)}」</span>
              <button type="button" className="px-x" onClick={() => close()}><X /></button>
            </div>
            <div className="px-pop-body">
              {phase === 'loading' ? <Loading variant={variant} /> : null}
              {phase === 'error' ? <div><div className="px-err">分析没能完成</div><div className="px-muted">模型服务暂时无法连接，请稍后再试。</div></div> : null}
              {phase === 'streaming' || phase === 'done' ? <Streamed variant={variant} md={text} bounds={bounds} done={phase === 'done'} /> : null}
            </div>
            {phase === 'streaming' ? <div className="px-pop-foot"><span className="px-muted">生成中…</span><span className="px-muted">Esc 关闭</span></div> : null}
            {phase === 'done' ? <div className="px-pop-foot px-in"><span className="px-muted">AI 生成，仅供参考</span><button type="button" className="px-primary" onClick={fix}><Pin />固定探索</button></div> : null}
            {phase === 'error' ? <div className="px-pop-foot"><span /><button type="button" className="px-secondary" onClick={analyse}><RotateCcw />重试</button></div> : null}
          </div>
        ) : null}
      </div>

      <div className="px-rail">
        <div className="px-note" style={{ top: 330 }}>
          <div className="px-note-q">每一步只做一件事，并且有明确…</div>
          <div className="px-note-t">这一条可以直接放进团队的设计规范里。</div>
        </div>
        {cardTop !== null && zoom === 0 ? (
          <button type="button" ref={cardRef} className={`px-card ${active ? 'on' : ''}`} style={{ top: cardTop }} onClick={() => setZoom(1)}>
            <div className="px-card-t"><Sparkles />{parts.title}</div>
            <div className="px-card-i">{parts.intro}</div>
          </button>
        ) : cardTop !== null ? <div className="px-card ghost" style={{ top: cardTop }} /> : null}
      </div>

      {zoom ? <ZoomLayer variant={variant} level={zoom} fromEl={() => cardRef.current ?? document.querySelector('.px-card.ghost')} parts={parts}
        onLevel={setZoom} onDelete={() => { passage.current?.replaceWith(...Array.from(passage.current.childNodes)); passage.current = null; setCardTop(null); setZoom(0); setPhase('idle'); }} /> : null}

      {confirm ? (
        <div className="px-scrim" onClick={() => setConfirm(false)}>
          <div className="px-dialog" onClick={(e) => e.stopPropagation()}>
            <b>丢弃这次分析？</b>
            <p>分析结果尚未固定，关闭后将丢弃。</p>
            <div className="px-dialog-acts"><button type="button" className="px-secondary" onClick={() => setConfirm(false)}>继续查看</button><button type="button" className="px-primary" onClick={() => close(true)}>丢弃</button></div>
          </div>
        </div>
      ) : null}

      <pre className="px-state">{JSON.stringify({ variant, phase, streamed: text.length, zoom, fixed: cardTop !== null, overlap }, null, 1)}</pre>
    </div>
  );
}

function Loading({ variant }: { variant: Variant }) {
  if (variant === 'C') return <Skeleton />;
  return <div><div className="px-muted">正在结合本讲上下文分析…</div><div className="px-sk" style={{ width: 170, height: 16, marginTop: 14 }} /><div className="px-sk" /><div className="px-sk" style={{ width: '88%' }} /><div className="px-sk" style={{ width: '64%' }} /></div>;
}

function Skeleton({ title = true, intro = true, body = true }: { title?: boolean; intro?: boolean; body?: boolean }) {
  return <>
    {title ? <div className="px-sk" style={{ width: 160, height: 18, marginTop: 0 }} /> : null}
    {intro ? <><div className="px-sk" style={{ marginTop: 14 }} /><div className="px-sk" style={{ width: '70%' }} /></> : null}
    {body ? <><div className="px-label" style={{ marginTop: 16 }}>概念阐述</div><div className="px-sk" /><div className="px-sk" /><div className="px-sk" style={{ width: '82%' }} /></> : null}
  </>;
}

function Streamed({ variant, md, bounds, done }: { variant: Variant; md: string; bounds: number[]; done: boolean }) {
  const { title, intro, body } = split(md);
  if (variant === 'D') {
    // each streamed chunk is its own span, keyed by where it starts in the
    // whole text, so only the new ones mount and fade from grey to ink
    const fade = (s: string) => {
      const at = md.indexOf(s); const out = []; let from = at;
      for (const end of bounds) { if (end <= from) continue; const to = Math.min(end, at + s.length); out.push(<span key={from} className="px-token">{md.slice(from, to)}</span>); from = to; if (from >= at + s.length) break; }
      return out;
    };
    return <div className="px-doc">
      {title ? <h3>{fade(title)}</h3> : <Skeleton intro={false} body={false} />}
      {intro ? <p className="px-intro">{fade(intro)}</p> : <Skeleton title={false} body={false} />}
      {body.length ? <><div className="px-label px-fade">概念阐述</div>{body.map((p, i) => <p key={i}>{fade(p)}</p>)}</> : <Skeleton title={false} intro={false} />}
    </div>;
  }
  const caret = done ? null : <i className="px-caret" />;
  // which section is still being written decides where the caret sits
  const at = body.length ? 'body' : intro ? 'intro' : 'title';
  if (variant === 'B') {
    const sentences = (s: string) => s.split(/(?<=[。：])/).filter(Boolean).map((x, i) => <span key={i} className="px-fade">{x}</span>);
    return <div className="px-doc">
      {title ? <h3 className="px-fade">{title}</h3> : null}
      {intro ? <p className="px-intro">{sentences(intro)}</p> : null}
      {body.length ? <div className="px-label px-fade">概念阐述</div> : null}
      {body.map((p, i) => <p key={i}>{sentences(p)}</p>)}
      {!done ? <span className="px-dots"><i /><i /><i /></span> : null}
    </div>;
  }
  if (variant === 'C') {
    return <div className="px-doc">
      {title ? <h3>{title}{at === 'title' ? caret : null}</h3> : <Skeleton intro={false} body={false} />}
      {intro ? <p className="px-intro">{intro}{at === 'intro' ? caret : null}</p> : <Skeleton title={false} body={false} />}
      {body.length ? <><div className="px-label">概念阐述</div>{body.map((p, i) => <p key={i}>{p}{at === 'body' && i === body.length - 1 ? caret : null}</p>)}</> : <Skeleton title={false} intro={false} />}
    </div>;
  }
  return <div className="px-doc">
    {title ? <h3>{title}{at === 'title' ? caret : null}</h3> : caret}
    {intro ? <p className="px-intro">{intro}{at === 'intro' ? caret : null}</p> : null}
    {body.length ? <div className="px-label">概念阐述</div> : null}
    {body.map((p, i) => <p key={i}>{p}{i === body.length - 1 ? caret : null}</p>)}
  </div>;
}

// ZoomLayer is the overlay for both zoom levels; each variant animates the
// card's box differently between the rail card, level 1 and level 2.
function ZoomLayer({ variant, level, fromEl, parts, onLevel, onDelete }: {
  variant: Variant; level: Zoom; fromEl: () => Element | null; parts: ReturnType<typeof split>;
  onLevel: (z: Zoom) => void; onDelete: () => void;
}) {
  const box = useRef<HTMLDivElement>(null);
  const shown = useRef<Zoom>(0);
  const [closing, setClosing] = useState(false);
  const [askDelete, setAskDelete] = useState(false);
  const reduce = window.matchMedia('(prefers-reduced-motion: reduce)').matches;

  const target = (z: Zoom): Rect => {
    const vw = innerWidth, vh = innerHeight;
    if (z === 2) { const w = Math.round(vw * 0.7), h = Math.round(vh * 0.7); return { left: (vw - w) / 2, top: (vh - h) / 2, width: w, height: h }; }
    const h = Math.min(Math.round(vh * 0.8), 620);
    if (variant === 'B' || variant === 'D') { const r = fromEl()!.getBoundingClientRect(); return { left: r.right - 640, top: Math.max(24, Math.min(r.top, vh - h - 24)), width: 640, height: h }; }
    return { left: (vw - 640) / 2, top: (vh - h) / 2, width: 640, height: h };
  };
  const rectOf = (el: Element): Rect => { const r = el.getBoundingClientRect(); return { left: r.left, top: r.top, width: r.width, height: r.height }; };
  const css = (r: Rect) => ({ left: `${r.left}px`, top: `${r.top}px`, width: `${r.width}px`, height: `${r.height}px` });

  const animate = (from: Rect, to: Rect, z: Zoom) => {
    const el = box.current!;
    Object.assign(el.style, css(to));
    if (reduce) return;
    if (variant === 'C' && shown.current === 0) {
      el.animate([{ transform: 'scale(.86)', opacity: 0 }, { transform: 'scale(1.015)', opacity: 1, offset: .7 }, { transform: 'scale(1)', opacity: 1 }], { duration: 420, easing: 'cubic-bezier(.2,.9,.3,1.2)' });
    } else {
      el.animate([css(from), css(to)], { duration: z === 2 ? 420 : 380, easing: 'cubic-bezier(.2,.8,.2,1)' });
    }
    el.querySelector('.px-zoom-content')?.animate([{ opacity: 0, transform: 'translateY(6px)' }, { opacity: 1, transform: 'none' }], { duration: 260, delay: 140, fill: 'backwards' });
  };

  useLayoutEffect(() => {
    const el = box.current!;
    const from = shown.current === 0 ? rectOf(fromEl()!) : rectOf(el);
    animate(from, target(level), level);
    shown.current = level;
  }, [level]);

  const close = () => {
    const el = box.current!, card = fromEl();
    setClosing(true);
    if (reduce || !card || level === 2 && variant !== 'A') { onLevel(0); return; }
    const a = variant === 'C'
      ? el.animate([{ transform: 'scale(1)', opacity: 1 }, { transform: 'scale(.9)', opacity: 0 }], { duration: 200, easing: 'ease-in' })
      : el.animate([css(rectOf(el)), css(rectOf(card))], { duration: 320, easing: 'cubic-bezier(.4,0,.2,1)' });
    el.querySelector('.px-zoom-content')?.animate([{ opacity: 1 }, { opacity: 0 }], { duration: 120, fill: 'forwards' });
    a.onfinish = () => onLevel(0);
  };

  return (
    <div className={`px-zoom lv${level} ${closing ? 'closing' : ''}`} onClick={close}>
      <div ref={box} className={`px-zoom-box lv${level}`} onClick={(e) => e.stopPropagation()}>
        {level === 1 ? (
          <div className="px-zoom-content">
            <div className="px-zoom-head">
              <span className="px-ex-tag"><Sparkles />探索</span>
              <span className="px-grow" />
              <button type="button" className="px-expand" onClick={() => onLevel(2)}><Telescope />展开探索</button>
              <button type="button" className="px-ghost" onClick={() => setAskDelete(true)}><Trash2 />删除</button>
              <button type="button" className="px-x" onClick={close}><X /></button>
            </div>
            <div className="px-zoom-scroll">
              <div className="px-src"><span className="px-src-q">Agent</span><span className="px-muted">出自 01｜用户输入规范化</span></div>
              <h2 className="px-doc-title">{parts.title}</h2>
              <p className="px-doc-intro">{parts.intro}</p>
              <hr />
              <h3 className="px-doc-h3">概念阐述</h3>
              {parts.body.map((p, i) => <p key={i} className="px-doc-p">{p}</p>)}
              <p className="px-doc-p"><b>和本讲的关系</b></p>
              <ul className="px-doc-ul"><li>每一步只做一件事，并且有明确的输入和输出</li><li>控制流是显式的，不依赖框架</li><li>失败时有可预期的回退路径</li></ul>
              <p className="px-doc-p">在后续几讲里，规划、执行和记忆会分别展开，它们都是 Agent 的组成部分。</p>
            </div>
            {askDelete ? (
              <div className="px-inline-confirm">
                <b>删除这个探索？</b><span className="px-muted">探索文档会一并删除，删除后无法恢复。</span>
                <span className="px-grow" />
                <button type="button" className="px-secondary" onClick={() => setAskDelete(false)}>取消</button>
                <button type="button" className="px-primary" onClick={onDelete}>删除</button>
              </div>
            ) : null}
          </div>
        ) : (
          <div className="px-zoom-content px-lv2">
            <div className="px-zoom-head">
              <Telescope className="px-ex" /><b>展开探索</b><span className="px-muted">· {parts.title}</span>
              <span className="px-grow" /><span className="px-chip">节点 0</span>
              <button type="button" className="px-x" onClick={close}><X /></button>
            </div>
            <div className="px-soon">
              <Telescope className="px-soon-icon" />
              <b>探索对话即将推出</b>
              <span className="px-muted">以这个探索为起点和 AI 深入讨论：沿主线追问，也可以向旁支扩展，讨论的结果能同步回探索文档。</span>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
