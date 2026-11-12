import ReactMarkdown, { defaultUrlTransform } from 'react-markdown';
import rehypeHighlight from 'rehype-highlight';
import rehypeRaw from 'rehype-raw';
import rehypeSanitize from 'rehype-sanitize';
import remarkGfm from 'remark-gfm';
import type { Components } from 'react-markdown';
import { cn } from '@/lib/utils';

// resolve turns a reference relative to the Markdown file into a URL; links
// that already carry a scheme or are absolute stay as they are.
function resolver(base?: string) {
  return (url: string) => {
    const safe = defaultUrlTransform(url);
    if (!base || !safe || /^[a-z][a-z0-9+.-]*:|^\/|^#/i.test(safe)) return safe;
    return new URL(safe, new URL(base, location.origin)).pathname;
  };
}

// Markdown renders course text like a documentation site. Raw HTML in the
// text is allowed but sanitized, since course text comes from uploaders.
export function Markdown({ text, base, className, components }: { text: string; base?: string; className?: string; components?: Components }) {
  return (
    <div className={cn('prose', className)}>
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        rehypePlugins={[rehypeRaw, rehypeSanitize, [rehypeHighlight, { detect: false, ignoreMissing: true }]]}
        urlTransform={resolver(base)}
        components={{
          a: ({ node: _n, ...p }) => <a {...p} target={p.href?.startsWith('#') ? undefined : '_blank'} rel="noreferrer" />,
          img: ({ node: _n, ...p }) => <img {...p} loading="lazy" alt={p.alt || ''} />,
          ...components,
        }}
      >
        {text}
      </ReactMarkdown>
    </div>
  );
}
