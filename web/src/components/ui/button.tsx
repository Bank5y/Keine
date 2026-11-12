import type { ComponentProps } from 'react';
import { cva, type VariantProps } from 'class-variance-authority';
import { Slot } from 'radix-ui';
import { cn } from '@/lib/utils';

const buttonVariants = cva(
  'inline-flex shrink-0 cursor-pointer items-center justify-center gap-2 rounded-[var(--radius)] font-medium tracking-[.02em] whitespace-nowrap transition-[background-color,color,border-color] duration-150 outline-none focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring disabled:cursor-not-allowed disabled:opacity-40 [&_svg]:size-4 [&_svg]:shrink-0',
  {
    variants: {
      variant: {
        default: 'border border-primary bg-primary text-primary-foreground not-disabled:hover:border-brand-hover not-disabled:hover:bg-brand-hover',
        outline: 'border border-foreground bg-transparent text-foreground not-disabled:hover:bg-foreground not-disabled:hover:text-background',
        ghost: 'border border-rule bg-transparent text-foreground not-disabled:hover:border-foreground',
        destructive: 'border border-rule bg-transparent text-foreground not-disabled:hover:border-danger not-disabled:hover:bg-danger not-disabled:hover:text-white',
        text: 'bg-transparent font-normal tracking-normal text-[13px] text-ink-3 not-disabled:hover:text-brand',
        round: 'rounded-full border border-transparent bg-transparent text-ink-2 not-disabled:hover:border-rule not-disabled:hover:text-foreground [&_svg]:size-[17px]',
        icon: 'rounded-full bg-surface/90 text-ink-2 shadow-[inset_0_0_0_1px_var(--rule)] not-disabled:hover:text-danger',
      },
      size: {
        default: 'h-[38px] px-[18px]',
        sm: 'h-[30px] px-3 text-[13px]',
        block: 'h-[46px] w-full px-[18px] text-[15px]',
        bare: 'p-0',
        icon: 'size-8 p-0',
        round: 'size-[34px] p-0',
      },
    },
    defaultVariants: { variant: 'outline', size: 'default' },
  },
);

export function Button({ className, variant, size, asChild = false, ...props }: ComponentProps<'button'> & VariantProps<typeof buttonVariants> & { asChild?: boolean }) {
  const Comp = asChild ? Slot.Root : 'button';
  return <Comp type={asChild ? undefined : (props.type ?? 'button')} className={cn(buttonVariants({ variant, size, className }))} {...props} />;
}
