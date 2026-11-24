import { useMemo, type ComponentProps } from 'react';
import type { Components } from 'react-markdown';
import { LazyMarkdown } from '@/components/lazy-markdown';
import { splitCitations, type Cited } from '@/lib/citations';

// a node of the Markdown tree, as far as citations go
type MdNode = { type: string; value?: string; url?: string; children?: MdNode[] };

const CITE = '#cite-';

// remarkCitations turns the citations in a reply's text into links to
// their sources (ADR 0013), each to be drawn as a tag; numbers it does not
// know go. Text inside a link or code is left as it is.
function remarkCitations(table: Map<number, Cited>) {
  const walk = (n: MdNode) => {
    if (!n.children || n.type === 'link' || n.type === 'linkReference') return;
    n.children = n.children.flatMap((c): MdNode[] => {
      if (c.type !== 'text') { walk(c); return [c]; }
      return splitCitations(c.value ?? '', table).map((p) => (typeof p === 'string'
        ? { type: 'text', value: p }
        : { type: 'link', url: `${CITE}${p.n}`, children: [{ type: 'text', value: `${p.host} · ${p.n}` }] }));
    });
  };
  return () => (tree: MdNode) => { walk(tree); };
}

// citedParts draw a citation as a tag, the site and the number, that opens
// the source in a new tab and names it when pointed at. Any other link of
// a reply is the model's own and shows as plain text.
function citedParts(table: Map<number, Cited>): Components {
  return {
    a: ({ href, children }) => {
      const c = href?.startsWith(CITE) ? table.get(Number(href.slice(CITE.length))) : undefined;
      if (!c) return <>{children}</>;
      return <a className="cite" href={c.url} target="_blank" rel="noreferrer noopener" title={c.title}>{c.host}<span aria-hidden> · </span>{c.n}</a>;
    },
  };
}

// CitedMarkdown is a reply of the dialogue: its citations drawn from table,
// what it may cite (citationTable).
export function CitedMarkdown({ text, table, components }: { text: string; table: Map<number, Cited> } & Pick<ComponentProps<typeof LazyMarkdown>, 'components'>) {
  const plugins = useMemo(() => [remarkCitations(table)], [table]);
  const parts = useMemo(() => ({ ...components, ...citedParts(table) }), [components, table]);
  return <LazyMarkdown text={text} components={parts} remarkPlugins={plugins} />;
}
