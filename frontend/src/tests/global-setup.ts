import { dumpActiveHandles } from "./utils";

export default async function globalSetup() {
  const watchdog = setTimeout(() => {
    // eslint-disable-next-line no-console
    console.warn("[vitest] run exceeded 55s watchdog");
    dumpActiveHandles("watchdog");
    process.exit(1);
  }, 55_000);

  return () => {
    clearTimeout(watchdog);
  };
}
