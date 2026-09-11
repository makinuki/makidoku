import { beforeEach, describe, expect, it } from "vite-plus/test";
import { MAX_CONCURRENT_COVERS, acquireCoverSlot, resetCoverQueue } from "./coverQueue";

const flush = () => new Promise((resolve) => setTimeout(resolve, 0));

describe("cover queue", () => {
  beforeEach(() => resetCoverQueue());

  it("grants no more than the concurrency limit", async () => {
    const granted: number[] = [];
    for (let index = 0; index < MAX_CONCURRENT_COVERS + 2; index += 1) {
      const slot = acquireCoverSlot();
      void slot.ready.then(() => granted.push(index));
    }
    await flush();
    expect(granted).toEqual([0, 1, 2]);
  });

  it("hands a released slot to the next waiter", async () => {
    const order: number[] = [];
    const slots = Array.from({ length: MAX_CONCURRENT_COVERS + 1 }, (_, index) => {
      const slot = acquireCoverSlot();
      void slot.ready.then(() => order.push(index));
      return slot;
    });
    await flush();
    expect(order).toEqual([0, 1, 2]);

    slots[0].release();
    await flush();
    expect(order).toEqual([0, 1, 2, 3]);
  });

  it("drops a cancelled waiter without holding a slot", async () => {
    const order: number[] = [];
    const held = Array.from({ length: MAX_CONCURRENT_COVERS }, (_, index) => {
      const slot = acquireCoverSlot();
      void slot.ready.then(() => order.push(index));
      return slot;
    });
    const pending = acquireCoverSlot();
    void pending.ready.then(() => order.push(99));
    await flush();
    expect(order).toEqual([0, 1, 2]);

    pending.cancel();
    held[0].release();
    await flush();
    expect(order).toEqual([0, 1, 2]);

    const next = acquireCoverSlot();
    void next.ready.then(() => order.push(4));
    await flush();
    expect(order).toEqual([0, 1, 2, 4]);
  });

  it("releases a granted slot when the holder cancels", async () => {
    const held = Array.from({ length: MAX_CONCURRENT_COVERS }, () => acquireCoverSlot());
    const pending = acquireCoverSlot();
    let granted = false;
    void pending.ready.then(() => {
      granted = true;
    });
    await flush();
    expect(granted).toBe(false);

    held[0].cancel();
    await flush();
    expect(granted).toBe(true);
  });
});
