import type { ReactNode } from 'react';
import { Check, ChevronDown } from 'lucide-react';
import { DropdownMenu as M } from 'radix-ui';
import { cn } from '@/lib/utils';

export interface FilterOption<T extends string> { value: T; label: string }

// FilterButton is one filter dimension on a narrow screen: a round button
// that opens a menu of 全部 and the options. Unfiltered it shows the
// dimension's name; filtered it shows the chosen option in the brand color.
// value '' is 全部.
export function FilterButton<T extends string>({ label, value, options, onChange }: {
  label: string; value: T | ''; options: FilterOption<T>[]; onChange: (value: T | '') => void;
}) {
  const chosen = options.find((o) => o.value === value);
  const items: FilterOption<T | ''>[] = [{ value: '', label: '全部' }, ...options];
  return (
    <M.Root modal={false}>
      <M.Trigger className={cn('filter-btn', chosen && 'chosen')} aria-label={chosen ? `${label}：${chosen.label}` : label}>
        <span>{chosen ? chosen.label : label}</span>
        <ChevronDown aria-hidden="true" />
      </M.Trigger>
      <M.Portal>
        <M.Content className="filter-menu" align="start" sideOffset={6} collisionPadding={16}>
          <M.RadioGroup value={chosen ? value : ''} onValueChange={(v) => onChange(v as T | '')}>
            {items.map((o) => (
              <M.RadioItem key={o.value || 'all'} value={o.value} className="filter-item">
                <span>{o.label}</span>
                <M.ItemIndicator><Check aria-hidden="true" /></M.ItemIndicator>
              </M.RadioItem>
            ))}
          </M.RadioGroup>
        </M.Content>
      </M.Portal>
    </M.Root>
  );
}

// FilterBar is the row of filter buttons with the result count at its end,
// e.g. count={6} unit="门课程"; a null count is still loading.
export function FilterBar({ children, count, unit }: { children: ReactNode; count: number | null; unit: string }) {
  return (
    <div className="filter-bar">
      <div className="filter-btns">{children}</div>
      {count === null ? null : <span className="filter-count"><b>{count}</b>{unit}</span>}
    </div>
  );
}
