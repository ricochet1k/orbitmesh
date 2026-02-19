/**
 * Synchronously find a free TCP port on 127.0.0.1 by spawning a tiny Node
 * one-liner.  Returns 0 on any error so callers can fall back to their own
 * default.
 */
import { execFileSync } from "node:child_process";

function allocateFreePort(): number {
  try {
    const result = execFileSync(
      process.execPath,
      [
        "--input-type=module",
        "--eval",
        `
import net from 'node:net';
const srv = net.createServer();
srv.listen(0, '127.0.0.1', () => {
  process.stdout.write(String(srv.address().port));
  srv.close();
});
`,
      ],
      { encoding: "utf-8", timeout: 5_000 },
    );
    return Number(result.trim());
  } catch {
    return 0;
  }
}

/**
 * Return the port stored in the given env variable, or allocate a new free
 * port and store it back into process.env so subsequent calls (e.g. from
 * globalSetup) return the same value.
 */
export function reservePort(envKey: string, fallback: number): number {
  const existing = process.env[envKey];
  if (existing) {
    return Number(existing);
  }
  const port = allocateFreePort() || fallback;
  process.env[envKey] = String(port);
  return port;
}
