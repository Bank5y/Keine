// pinHeading scrolls a heading to the top of the page and keeps it there
// while the page is still settling. The images in a lesson load lazily, so a
// jump far down the text passes images that only start loading as the scroll
// reaches them; each one pushes the heading further down after the scroll
// already set off for its old place. The pin follows the heading until the
// page has stopped changing for a while, and lets go as soon as the reader
// scrolls by hand (wheel, touch, keys or the scrollbar), never fighting them.
// onRelease is told when the reader takes over, even after the pin settled,
// so a caller can treat the heading as current until then. It returns a
// function that lets go without telling.

// how long the heading's place must stay unchanged before the pin lets go,
// and the longest it holds on in any case
const SETTLE_MS = 1000;
const MAX_MS = 8000;

const USER_EVENTS = ['wheel', 'touchstart', 'keydown', 'mousedown'] as const;

export function pinHeading(el: HTMLElement, behavior: ScrollBehavior, onRelease?: () => void): () => void {
  const start = performance.now();
  let lastChange = start;
  let frame = 0;
  let done = false;
  let still = 0; // frames the scroll has been at rest
  // where the heading sits in the page and where the page is scrolled, as of
  // the previous frame
  let place = NaN;
  let scrolled = NaN;

  const go = (b: ScrollBehavior) => el.scrollIntoView({ block: 'start', behavior: b });
  const offTarget = () => {
    const margin = parseFloat(getComputedStyle(el).scrollMarginTop) || 0;
    const atBottom = window.scrollY + window.innerHeight >= document.documentElement.scrollHeight - 1;
    return Math.abs(el.getBoundingClientRect().top - margin) > 1 && !atBottom;
  };

  const tick = () => {
    frame = 0;
    if (done) return;
    if (!el.isConnected) { stop(); return; }
    const now = performance.now();
    const p = el.getBoundingClientRect().top + window.scrollY;
    const y = window.scrollY;
    if (p !== place && !Number.isNaN(place)) {
      // the page above the heading changed: head for its new place (a smooth
      // scroll in flight takes the new target)
      lastChange = now;
      go(behavior);
    } else if (y === scrolled && ++still > 2 && offTarget()) {
      // the scroll came to rest short of the heading (it set off before the
      // page grew, or something else moved it): put it right at once
      lastChange = now;
      go('instant');
    }
    if (y !== scrolled) still = 0;
    place = p;
    scrolled = y;
    if (now - lastChange > SETTLE_MS || now - start > MAX_MS) return; // settled
    frame = requestAnimationFrame(tick);
  };

  const byUser = () => { stop(); onRelease?.(); };
  function stop() {
    if (done) return;
    done = true;
    cancelAnimationFrame(frame);
    USER_EVENTS.forEach((t) => window.removeEventListener(t, byUser, true));
  }

  USER_EVENTS.forEach((t) => window.addEventListener(t, byUser, { capture: true, passive: true }));
  go(behavior);
  frame = requestAnimationFrame(tick);
  return stop;
}
