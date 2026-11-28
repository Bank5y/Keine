import { useEffect, useState } from 'react';

// useMedia tells whether the media query matches, and follows it as it changes.
export function useMedia(query: string) {
  const [on, setOn] = useState(() => matchMedia(query).matches);
  useEffect(() => {
    const m = matchMedia(query);
    const onChange = () => setOn(m.matches);
    onChange();
    m.addEventListener('change', onChange);
    return () => m.removeEventListener('change', onChange);
  }, [query]);
  return on;
}
