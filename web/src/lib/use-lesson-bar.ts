import { useEffect, useReducer } from 'react';
import { initialBar, nextBar, type TapOn } from '@/lib/lesson-bar';

// A handler that answers a tap no element can show for (the annotator, on
// annotated text painted as a CSS highlight) claims it, so the lesson bar
// leaves it alone.
const claimed = new WeakSet<Event>();
export const claimTap = (e: Event) => { claimed.add(e); };

function tapOn(e: MouseEvent): TapOn {
  const t = e.target instanceof Element ? e.target : null;
  if (claimed.has(e) || t?.closest('.ann-bar, .note-card')) return 'annotation';
  if (t?.closest('a')) return 'link';
  if (t?.closest('button, input, textarea, select, label, summary, [role=button], [role=tab]')) return 'button';
  if (t?.closest('video, .video-frame')) return 'video';
  if (t?.closest('img, svg')) return 'image';
  return 'page';
}

// useLessonBar says whether the lesson's top bar shows, from the page's
// scrolling and the reader's taps on it (see lesson-bar.ts). Off, it does
// not listen and the bar shows.
export function useLessonBar(on: boolean) {
  const [bar, dispatch] = useReducer(nextBar, initialBar);
  useEffect(() => {
    if (!on) return undefined;
    const onScroll = () => dispatch({ type: 'scroll', y: scrollY });
    // taps outside the page (a drawer, a dialog) are not on the text
    const onClick = (e: MouseEvent) => {
      if (!(e.target instanceof Node) || !document.getElementById('view')?.contains(e.target)) return;
      const s = getSelection();
      dispatch({ type: 'tap', y: e.clientY, height: innerHeight, on: tapOn(e), selecting: !!s && !s.isCollapsed });
    };
    onScroll();
    addEventListener('scroll', onScroll, { passive: true });
    document.addEventListener('click', onClick);
    return () => { removeEventListener('scroll', onScroll); document.removeEventListener('click', onClick); };
  }, [on]);
  return !on || bar.shown;
}
