export const dumpActiveHandles = (label: string) => {
  const getter = (process as any)?._getActiveHandles;
  if (typeof getter !== "function") return;
  const handles = getter.call(process) as unknown[];
  const summary = handles.map((handle) => handle?.constructor?.name ?? "Unknown");
  if (summary.length === 0) return;
  // eslint-disable-next-line no-console
  console.warn(`[vitest] ${label}: active handles`, summary);
};
