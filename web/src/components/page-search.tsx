import { ChevronRight, Search } from 'lucide-react';
import { libraryHref } from '@/lib/library-filter';
import { hitParts } from '@/lib/page-search';
import { Link } from '@/lib/router';

// Hit is text with what a search matched in it marked; one span, so a
// flex row keeps it as one piece of text.
export function Hit({ text, q }: { text: string; q: string }) {
  return <span>{hitParts(text, q).map((p, i) => (p.hit ? <mark key={i} className="search-hit">{p.text}</mark> : p.text))}</span>;
}

// SearchEnd closes a page's search results: why there are none (none),
// and the way to search the whole library for the same words.
export function SearchEnd({ q, none }: { q: string; none: string | null }) {
  return (
    <div className="search-else">
      {none ? <p className="search-none">{none}</p> : null}
      <Link href={libraryHref({ q })}><Search aria-hidden="true" />在课程库中搜索「{q}」<ChevronRight aria-hidden="true" /></Link>
    </div>
  );
}
