import { cn } from '@/lib/utils';

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
