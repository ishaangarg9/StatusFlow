import { clsx, type ClassValue } from "clsx"
import { twMerge } from "tailwind-merge"

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

// Shared timestamp formatters so every screen renders dates consistently.
// formatDate is date-only; formatDateTime includes the time.
export function formatDate(ts: string) {
  return new Date(ts).toLocaleDateString()
}

export function formatDateTime(ts: string) {
  return new Date(ts).toLocaleString()
}
