import { cn } from "@/lib/utils";

// Shared status pill used across monitors, incidents, and the public page.
// Maps a status string to a semantic color.
const STYLES: Record<string, string> = {
  // monitor / component health
  up: "bg-success/15 text-success",
  operational: "bg-success/15 text-success",
  down: "bg-destructive/15 text-destructive",
  degraded: "bg-warning/15 text-warning",
  paused: "bg-muted text-muted-foreground",
  // incident lifecycle
  open: "bg-destructive/15 text-destructive",
  resolved: "bg-success/15 text-success",
  investigating: "bg-warning/15 text-warning",
  identified: "bg-warning/15 text-warning",
  monitoring: "bg-warning/15 text-warning",
};

export function StatusBadge({
  status,
  className,
}: {
  status: string;
  className?: string;
}) {
  return (
    <span
      className={cn(
        "inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium capitalize",
        STYLES[status] ?? "bg-muted text-muted-foreground",
        className,
      )}
    >
      {status}
    </span>
  );
}
