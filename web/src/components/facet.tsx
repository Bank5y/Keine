import { cn } from '@/lib/utils';

// facetItems are a facet's items for a list of options: 全部 ('') first.
export const facetItems = (options: { value: string; label: string }[]): [string, string][] =>
  [['', '全部'], ...options.map((o): [string, string] => [o.value, o.label])];

// Facet is one row of text filters, the editorial way.
export function Facet({ label, value, items, onPick }: { label: string; value: string; items: [string, string][]; onPick: (v: string) => void }) {
  return (
    <div className="facet">
      <span className="facet-label">{label}</span>
      <div className="facet-items">
        {items.map(([v, t]) => <button key={v} type="button" className={cn('fi', v === value && 'active')} onClick={() => onPick(v)}>{t}</button>)}
      </div>
    </div>
  );
}
