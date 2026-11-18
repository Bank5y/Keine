import { useEffect, useState } from 'react';

// useKeyboardInset tells how much of the bottom of the layout viewport the
// on-screen keyboard covers, in px (0 with no keyboard). Phone browsers shrink
// only the visual viewport for the keyboard, so a bar fixed to the bottom
// would sit behind it unless lifted by this much.
export function useKeyboardInset(on: boolean) {
  const [inset, setInset] = useState(0);
  useEffect(() => {
    const vv = window.visualViewport;
    if (!on || !vv) { setInset(0); return undefined; }
    const update = () => setInset(Math.max(0, Math.round(innerHeight - vv.height - vv.offsetTop)));
    update();
    vv.addEventListener('resize', update);
    vv.addEventListener('scroll', update);
    return () => { vv.removeEventListener('resize', update); vv.removeEventListener('scroll', update); };
  }, [on]);
  return inset;
}
