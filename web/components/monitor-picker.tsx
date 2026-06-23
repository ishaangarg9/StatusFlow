"use client";

import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api/client";
import { qk } from "@/lib/query-keys";
import { Checkbox } from "@/components/ui/checkbox";
import { Skeleton } from "@/components/ui/skeleton";

// Checkbox list of the org's monitors. Selection is a controlled array of ids.
export function MonitorPicker({
  orgId,
  selected,
  onChange,
}: {
  orgId: string;
  selected: string[];
  onChange: (ids: string[]) => void;
}) {
  const { data: monitors, isLoading } = useQuery({
    queryKey: qk.monitors(orgId),
    queryFn: () => api.listMonitors(orgId),
  });

  if (isLoading) return <Skeleton className="h-24 w-full" />;
  if (!monitors || monitors.length === 0) {
    return (
      <p className="text-sm text-muted-foreground">
        No monitors to add yet.
      </p>
    );
  }

  function toggle(id: string, checked: boolean) {
    onChange(checked ? [...selected, id] : selected.filter((x) => x !== id));
  }

  return (
    <div className="space-y-2 rounded-md border p-3">
      {monitors.map((m) => (
        <label
          key={m.id}
          className="flex cursor-pointer items-center gap-3 text-sm"
        >
          <Checkbox
            checked={selected.includes(m.id)}
            onCheckedChange={(c) => toggle(m.id, Boolean(c))}
          />
          <span className="font-medium">{m.name}</span>
          <span className="truncate text-muted-foreground">{m.url}</span>
        </label>
      ))}
    </div>
  );
}
