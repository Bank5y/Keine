import { clsx, type ClassValue } from 'clsx';
import { twMerge } from 'tailwind-merge';

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

// reducedMotion tells whether the reader asked for less motion.
// highlightsSupported tells whether the browser paints CSS Custom Highlights.
export const highlightsSupported = () => typeof CSS !== 'undefined' && 'highlights' in CSS;

// reducedMotion tells whether the reader asked for less motion.
export const reducedMotion = () => window.matchMedia('(prefers-reduced-motion: reduce)').matches;
