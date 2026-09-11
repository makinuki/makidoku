// coverQueue bounds how many cover images the client requests at once. A cover
// route proxies the source's own image, which can be several megabytes, and a
// browser only opens a handful of connections per origin. An unbounded burst
// therefore holds every connection for the length of a download and starves
// unrelated requests, including navigation to another page. Reserving most
// connections for the API keeps the app responsive while the grid fills in.
export const MAX_CONCURRENT_COVERS = 3;

type Waiter = { cancelled: boolean; started: boolean; start: () => void };

let active = 0;
const waiting: Waiter[] = [];

function pump() {
  while (active < MAX_CONCURRENT_COVERS && waiting.length > 0) {
    const waiter = waiting.shift()!;
    if (waiter.cancelled) continue;
    active += 1;
    waiter.started = true;
    waiter.start();
  }
}

export type CoverSlot = {
  // ready resolves once the slot may make its request.
  ready: Promise<void>;
  // release returns the slot; safe to call more than once.
  release: () => void;
  // cancel abandons a pending or granted slot, e.g. when the element unmounts.
  cancel: () => void;
};

export function acquireCoverSlot(): CoverSlot {
  const waiter: Waiter = { cancelled: false, started: false, start: () => {} };
  const ready = new Promise<void>((resolve) => {
    waiter.start = () => resolve();
  });
  waiting.push(waiter);
  pump();

  let released = false;
  const release = () => {
    if (released || !waiter.started) return;
    released = true;
    active = Math.max(0, active - 1);
    pump();
  };

  return {
    ready,
    release,
    cancel: () => {
      waiter.cancelled = true;
      if (!waiter.started) {
        const index = waiting.indexOf(waiter);
        if (index >= 0) waiting.splice(index, 1);
        return;
      }
      release();
    },
  };
}

// resetCoverQueue clears the scheduler. Tests that abandon covers use it so one
// case cannot leave a slot reserved for the next.
export function resetCoverQueue() {
  active = 0;
  waiting.length = 0;
}
