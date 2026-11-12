import type { ComponentProps } from 'react';
import { cn } from '@/lib/utils';

export function Input({ className, ...props }: ComponentProps<'input'>) {
  return (
    <input
      className={cn('h-11 min-w-0 rounded-[var(--radius)] border border-input bg-surface px-3 text-sm transition-[border-color] duration-150 outline-none placeholder:text-ink-3 focus:border-foreground disabled:cursor-not-allowed disabled:opacity-50', className)}
      {...props}
    />
  );
}
