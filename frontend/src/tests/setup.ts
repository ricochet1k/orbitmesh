import { afterAll, afterEach, beforeAll } from "vitest";

type TimerHandle = ReturnType<typeof setTimeout>;

const timeouts = new Set<TimerHandle>();
const intervals = new Set<TimerHandle>();

const originalTimers = {
  setTimeout: globalThis.setTimeout,
  clearTimeout: globalThis.clearTimeout,
  setInterval: globalThis.setInterval,
  clearInterval: globalThis.clearInterval,
};

const dumpActiveHandles = (label: string) => {
  const getter = (process as any)?._getActiveHandles;
  if (typeof getter !== "function") return;
  const handles = getter.call(process) as unknown[];
  const summary = handles.map((handle) => handle?.constructor?.name ?? "Unknown");
  if (summary.length === 0) return;
  // eslint-disable-next-line no-console
  console.warn(`[vitest] ${label}: active handles`, summary);
};

const alwaysDump = process.env.VITEST_DEBUG_HANDLES === "1";

beforeAll(() => {
  globalThis.setTimeout = ((handler: TimerHandler, timeout?: number, ...args: any[]) => {
    const id = originalTimers.setTimeout(handler, timeout, ...args);
    timeouts.add(id);
    return id;
  }) as typeof setTimeout;

  globalThis.clearTimeout = ((id?: TimerHandle) => {
    if (id) timeouts.delete(id);
    return originalTimers.clearTimeout(id as any);
  }) as typeof clearTimeout;

  globalThis.setInterval = ((handler: TimerHandler, timeout?: number, ...args: any[]) => {
    const id = originalTimers.setInterval(handler, timeout, ...args);
    intervals.add(id);
    return id;
  }) as typeof setInterval;

  globalThis.clearInterval = ((id?: TimerHandle) => {
    if (id) intervals.delete(id);
    return originalTimers.clearInterval(id as any);
  }) as typeof clearInterval;

});

afterEach(() => {
  for (const id of timeouts) {
    originalTimers.clearTimeout(id as any);
  }
  timeouts.clear();

  for (const id of intervals) {
    originalTimers.clearInterval(id as any);
  }
  intervals.clear();
});

afterAll(() => {
  if (alwaysDump && (timeouts.size > 0 || intervals.size > 0)) {
    // eslint-disable-next-line no-console
    console.warn(`[vitest] pending timers after run: ${timeouts.size} timeouts, ${intervals.size} intervals`);
    dumpActiveHandles("afterAll");
  }
  globalThis.setTimeout = originalTimers.setTimeout;
  globalThis.clearTimeout = originalTimers.clearTimeout;
  globalThis.setInterval = originalTimers.setInterval;
  globalThis.clearInterval = originalTimers.clearInterval;
});
