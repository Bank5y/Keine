import { lazy, Suspense, type ComponentProps } from 'react';
import { Skeleton } from '@/components/skeleton';

// The Markdown renderer (with syntax highlighting) is the heaviest part of
// the app, so it is loaded the first time some course text is shown.
const Markdown = lazy(() => import('@/components/markdown').then((m) => ({ default: m.Markdown })));

export function LazyMarkdown(props: ComponentProps<typeof Markdown>) {
  return (
    <Suspense fallback={<div className="space-y-4 py-4">{Array.from({ length: 6 }).map((_, i) => <Skeleton key={i} className="h-4" />)}</div>}>
      <Markdown {...props} />
    </Suspense>
  );
}
