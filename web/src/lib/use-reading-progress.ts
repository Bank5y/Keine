import { useEffect, useRef, useState, type RefObject } from 'react';
import { api } from '@/lib/api';
import type { LessonState, LessonText } from '@/lib/courses';

const SAVE_EVERY_MS = 2000;

const scrollRatio = () => {
  const max = document.documentElement.scrollHeight - window.innerHeight;
  return max > 0 ? Math.min(1, window.scrollY / max) : 0;
};

// useReadingProgress records studying a lesson: opening it, where the reader
// is and finishing it. For a text lesson the position is the scroll ratio and
// reaching the end of the text finishes it; a video lesson leaves both to its
// player (position in seconds, via savePosition and autoDone). Undoing "done"
// by hand turns automatic marking off until the lesson is opened again.
export function useReadingProgress(courseId: number, lesson: LessonText | null, doc: RefObject<HTMLElement | null>, onChange: () => void) {
  const [state, setStateValue] = useState<LessonState>('unstarted');
  const current = useRef<LessonState>('unstarted');
  const setState = (v: LessonState) => { current.current = v; setStateValue(v); };
  const manual = useRef(false);
  const url = lesson ? `/api/courses/${courseId}/lessons/${lesson.id}` : '';
  const text = !!lesson && !lesson.video;

  // opening: mark studying and jump back to where the reader left off
  useEffect(() => {
    if (!lesson) return undefined;
    manual.current = false;
    setState(lesson.state === 'unstarted' ? 'studying' : lesson.state);
    api('POST', url + '/visit').then(onChange).catch(() => {});
    if (text && lesson.position > 0 && lesson.position < 1) {
      // the text renders lazily and images grow it; restore once it settled
      const restore = () => window.scrollTo(0, lesson.position * (document.documentElement.scrollHeight - window.innerHeight));
      const t1 = setTimeout(restore, 150);
      const t2 = setTimeout(restore, 900);
      return () => { clearTimeout(t1); clearTimeout(t2); };
    }
    return undefined;
  }, [lesson?.id]); // eslint-disable-line react-hooks/exhaustive-deps

  // position: save while scrolling and when leaving
  useEffect(() => {
    if (!text) return undefined;
    let last = -1;
    const save = () => {
      const p = Math.round(scrollRatio() * 1000) / 1000;
      if (Math.abs(p - last) < 0.01) return;
      last = p;
      void api('PUT', url + '/progress', { position: p }).catch(() => {});
    };
    const timer = setInterval(save, SAVE_EVERY_MS);
    return () => { clearInterval(timer); save(); };
  }, [lesson?.id]); // eslint-disable-line react-hooks/exhaustive-deps

  // done: when the reader scrolls the end of the text into view (a short
  // text that fits on screen is marked by hand)
  useEffect(() => {
    if (!text) return undefined;
    let frame = 0;
    const check = () => {
      frame = 0;
      const last = doc.current?.querySelector('.reader-prose > :last-child');
      if (last && last.getBoundingClientRect().bottom <= window.innerHeight - 40) autoDone();
    };
    const onScroll = () => { if (!frame) frame = requestAnimationFrame(check); };
    window.addEventListener('scroll', onScroll, { passive: true });
    return () => { window.removeEventListener('scroll', onScroll); cancelAnimationFrame(frame); };
  }, [lesson?.id]); // eslint-disable-line react-hooks/exhaustive-deps

  // autoDone finishes the lesson unless the reader undid that by hand
  function autoDone() {
    if (!lesson || manual.current || current.current === 'done') return;
    setState('done');
    void api('PUT', url + '/progress', { done: true }).then(onChange).catch(() => {});
  }

  function savePosition(position: number) {
    if (lesson) void api('PUT', url + '/progress', { position }).catch(() => {});
  }

  async function setDone(done: boolean) {
    manual.current = true;
    setState(done ? 'done' : 'studying');
    await api('PUT', url + '/progress', { done }).catch(() => {});
    onChange();
  }

  return { state, setDone, autoDone, savePosition };
}
