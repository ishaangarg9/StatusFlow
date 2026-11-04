import { Skeleton } from "@/components/ui/skeleton";

// Shown inside the org shell (sidebar stays put) while a page's server data
// loads, instead of the previous page sitting frozen with no feedback.
export default function Loading() {
  return (
    <div className="mx-auto w-full max-w-5xl space-y-6 px-6 py-8">
      <Skeleton className="h-8 w-48" />
      <Skeleton className="h-64 w-full" />
    </div>
  );
}
