import { useEffect, useRef, useState, type ComponentProps } from 'react';
import { Check, Copy } from 'lucide-react';
import type { ExtraProps } from 'react-markdown';

// language reads the fence's language from the code element inside a <pre>;
// a fence without one has none, and is shown as plain text.
function language(node: ExtraProps['node']): string {
  const code = node?.children.find((c) => c.type === 'element' && c.tagName === 'code');
  if (!code || code.type !== 'element') return '';
  const cls = code.properties.className;
  const list = Array.isArray(cls) ? cls.map(String) : [];
  return list.find((c) => c.startsWith('language-'))?.slice('language-'.length) ?? '';
}

// CodeBlock frames a fenced code block with a title bar: the language it is
// written in, and a button that copies the code as written.
export function CodeBlock({ node, children, ...rest }: ComponentProps<'pre'> & ExtraProps) {
  const pre = useRef<HTMLPreElement>(null);
  const [copied, setCopied] = useState(false);
  const lang = language(node);

  useEffect(() => {
    if (!copied) return;
    const t = setTimeout(() => setCopied(false), 1600);
    return () => clearTimeout(t);
  }, [copied]);

  const copy = async () => {
    try {
      await navigator.clipboard.writeText((pre.current?.textContent ?? '').replace(/\n$/, ''));
      setCopied(true);
    } catch { /* clipboard unavailable (e.g. plain http): no feedback */ }
  };

  return (
    <div className="code-block">
      {/* data-chrome: not lesson text, so annotations skip it */}
      <div className="code-bar" data-chrome>
        {lang && <span className="code-lang">{lang}</span>}
        <button type="button" className="code-copy" onClick={copy} aria-label={copied ? '已复制' : '复制代码'}>
          {copied ? <Check size={13} /> : <Copy size={13} />}
          <span>{copied ? '已复制' : '复制'}</span>
        </button>
      </div>
      <pre ref={pre} {...rest}>{children}</pre>
    </div>
  );
}
