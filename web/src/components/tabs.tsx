import { useLayoutEffect, useRef, useState, type ReactNode } from 'react';
import { cn } from '@/lib/utils';

const reducedMotion = () => matchMedia('(prefers-reduced-motion: reduce)').matches;

// Tabs is a tablist whose underline (.tab-ink) slides to the chosen tab. The
// ink is placed against its offset parent, so the stylesheet decides which
// line it rides on.
export function Tabs<T extends string>({ value, onChange, items, className, tabClassName }: {
  value: T;
  onChange: (v: T) => void;
  items: [T, ReactNode][];
  className?: string;
  tabClassName?: string;
}) {
  const list = useRef<HTMLDivElement>(null);
  const ink = useRef<HTMLSpanElement>(null);
  useLayoutEffect(() => {
    const place = () => {
      const tab = list.current?.querySelector<HTMLElement>('[aria-selected="true"]');
      const box = ink.current?.offsetParent;
      if (!tab || !box || !ink.current) return;
      const t = tab.getBoundingClientRect();
      ink.current.style.width = `${t.width}px`;
      ink.current.style.transform = `translateX(${t.left - box.getBoundingClientRect().left - box.clientLeft}px)`;
    };
    place();
    const ro = new ResizeObserver(place);
    if (list.current) { ro.observe(list.current); list.current.querySelectorAll('[role=tab]').forEach((b) => ro.observe(b)); }
    return () => ro.disconnect();
  }, [value]);
  return (
    <div className={className} role="tablist" ref={list}>
      {items.map(([k, label]) => (
        <button key={k} role="tab" aria-selected={value === k} className={cn(tabClassName, value === k && 'active')} onClick={() => onChange(k)}>{label}</button>
      ))}
      <span className="tab-ink" ref={ink} aria-hidden />
    </div>
  );
}

// useTabFade lags the shown tab behind the chosen one: the old content fades
// out, then the new one fades in (about 150ms in all). Without motion it swaps
// at once.
export function useTabFade<T>(value: T, half = 75): [shown: T, fading: boolean] {
  const [shown, setShown] = useState(value);
  useLayoutEffect(() => {
    if (value === shown) return;
    if (reducedMotion()) { setShown(value); return; }
    const t = setTimeout(() => setShown(value), half);
    return () => clearTimeout(t);
  }, [value, shown, half]);
  return [shown, value !== shown];
}

export function TabPane({ fading, className, children }: { fading: boolean; className?: string; children: ReactNode }) {
  return <div className={cn('tab-pane', fading && 'fading', className)}>{children}</div>;
}
