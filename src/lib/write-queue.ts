type Work<T> = () => Promise<T> | T;

const _tails = new Map<string, Promise<unknown>>();
const _depth = new Map<string, number>();

export function getWriteQueueDepth(dbName: string): number {
  return _depth.get(dbName) ?? 0;
}

export function enqueueDbWrite<T>(dbName: string, work: Work<T>): Promise<T> {
  const tail = _tails.get(dbName) ?? Promise.resolve();
  _depth.set(dbName, (getWriteQueueDepth(dbName) + 1));

  const next = tail
    .catch(() => undefined)
    .then(() => work())
    .finally(() => {
      const current = getWriteQueueDepth(dbName);
      if (current <= 1) _depth.delete(dbName);
      else _depth.set(dbName, current - 1);
    });

  _tails.set(dbName, next);
  return next;
}
