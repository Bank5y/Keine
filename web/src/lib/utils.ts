import { clsx, type ClassValue } from 'clsx';
import { twMerge } from 'tailwind-merge';

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

// reducedMotion tells whether the reader asked for less motion.
export const reducedMotion = () => window.matchMedia('(prefers-reduced-motion: reduce)').matches;
