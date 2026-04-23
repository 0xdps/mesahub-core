export default function AdminLoading() {
  return (
    <div className="flex-1 overflow-auto">
      <div className="max-w-6xl mx-auto w-full px-6 py-6">
        {/* Stats row skeleton */}
        <div className="flex items-center gap-6 mb-6">
          <div className="h-3 w-64 bg-zinc-800 rounded-full animate-pulse" />
        </div>

        {/* Panel skeleton */}
        <div className="border border-zinc-800 rounded-xl overflow-hidden bg-zinc-950">
          {/* Tab bar skeleton */}
          <div className="flex items-center border-b border-zinc-800 px-4 py-3 gap-6">
            <div className="h-4 w-24 bg-zinc-800 rounded animate-pulse" />
            <div className="h-4 w-20 bg-zinc-800 rounded animate-pulse" />
            <div className="h-4 w-16 bg-zinc-800 rounded animate-pulse" />
          </div>

          {/* Table skeleton */}
          <div className="divide-y divide-zinc-900">
            {Array.from({ length: 6 }).map((_, i) => (
              <div key={i} className="flex items-center gap-6 px-4 py-3">
                <div className="h-4 w-36 bg-zinc-800 rounded animate-pulse" />
                <div className="h-4 w-24 bg-zinc-800 rounded animate-pulse" />
                <div className="h-4 w-16 bg-zinc-800 rounded animate-pulse" />
                <div className="h-4 w-20 bg-zinc-800 rounded animate-pulse" />
                <div className="h-3 w-12 bg-zinc-800 rounded animate-pulse ml-auto" />
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}
