// Outcome is what one item of a batch came to: the task's value, or the
// error it failed with.
export type Outcome<T, R> = { item: T; ok: true; value: R } | { item: T; ok: false; error: unknown };

// runInTurn runs task on each item, one after another (a server takes one
// course package or one publish at a time, and the reader watches them go
// by), and goes on past a failure; onEach hears of each outcome as it comes.
export async function runInTurn<T, R>(items: T[], task: (item: T, index: number) => Promise<R>, onEach?: (o: Outcome<T, R>, index: number) => void): Promise<Outcome<T, R>[]> {
  const out: Outcome<T, R>[] = [];
  for (const [i, item] of items.entries()) {
    let o: Outcome<T, R>;
    try {
      o = { item, ok: true, value: await task(item, i) };
    } catch (error) {
      o = { item, ok: false, error };
    }
    out.push(o);
    onEach?.(o, i);
  }
  return out;
}
