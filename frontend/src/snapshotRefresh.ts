// Changes invalidate the view; they are not a queue of snapshots to replay.
// Keep at most one request in flight and one trailing refresh, at up to 20 fps.
export function snapshotRefresh<T>(
  load: () => Promise<T>,
  apply: (value: T) => void,
  onError: (error: unknown) => void,
) {
  let disposed = false;
  let inFlight = false;
  let dirty = false;
  let lastStarted = -Infinity;
  let timer: ReturnType<typeof setTimeout> | undefined;
  const request = () => {
    if (disposed) return;
    dirty = true;
    if (inFlight || timer !== undefined) return;
    const delay = Math.max(0, 50 - (performance.now() - lastStarted));
    if (delay > 0) timer = setTimeout(start, delay);
    else start();
  };
  const start = () => {
    timer = undefined;
    if (disposed) return;
    dirty = false;
    inFlight = true;
    lastStarted = performance.now();
    void Promise.resolve()
      .then(load)
      .then((value) => {
        if (!disposed) apply(value);
      })
      .catch((error) => {
        if (!disposed) onError(error);
      })
      .finally(() => {
        inFlight = false;
        if (dirty) request();
      });
  };
  return {
    request,
    dispose() {
      disposed = true;
      if (timer !== undefined) clearTimeout(timer);
    },
  };
}
