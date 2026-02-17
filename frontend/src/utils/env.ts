export const isTestEnv = (): boolean =>
  (typeof import.meta !== "undefined" && import.meta.env?.MODE === "test") ||
  (typeof process !== "undefined" && Boolean(process.env?.VITEST))
