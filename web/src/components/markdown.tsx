import ReactMarkdown, { defaultUrlTransform } from 'react-markdown';
import rehypeHighlight from 'rehype-highlight';
import rehypeRaw from 'rehype-raw';
import rehypeSanitize from 'rehype-sanitize';
import rehypeSlug from 'rehype-slug';
import remarkGfm from 'remark-gfm';
import type { Components } from 'react-markdown';
import { common } from 'lowlight';
import dart from 'highlight.js/lib/languages/dart';
import dockerfile from 'highlight.js/lib/languages/dockerfile';
import elixir from 'highlight.js/lib/languages/elixir';
import groovy from 'highlight.js/lib/languages/groovy';
import haskell from 'highlight.js/lib/languages/haskell';
import http from 'highlight.js/lib/languages/http';
import nginx from 'highlight.js/lib/languages/nginx';
import powershell from 'highlight.js/lib/languages/powershell';
import protobuf from 'highlight.js/lib/languages/protobuf';
import scala from 'highlight.js/lib/languages/scala';
import { CodeBlock } from '@/components/code-block';
import { cn } from '@/lib/utils';

// highlight.js's common set already covers go, rust, sql, yaml, bash and the
// like; these add other languages course text often uses.
const languages = { ...common, dart, dockerfile, elixir, groovy, haskell, http, nginx, powershell, protobuf, scala };

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
        rehypePlugins={[rehypeRaw, rehypeSanitize, rehypeSlug, [rehypeHighlight, { detect: false, ignoreMissing: true, languages }]]}
        urlTransform={resolver(base)}
        components={{
          a: ({ node: _n, ...p }) => <a {...p} target={p.href?.startsWith('#') ? undefined : '_blank'} rel="noreferrer" />,
          pre: CodeBlock,
          img: ({ node: _n, ...p }) => <img {...p} loading="lazy" alt={p.alt || ''} />,
          ...components,
        }}
      >
        {text}
      </ReactMarkdown>
    </div>
  );
}
