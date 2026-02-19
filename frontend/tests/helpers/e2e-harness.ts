import { spawn, spawnSync, type ChildProcess } from "node:child_process";
import { createWriteStream, promises as fs } from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { reservePort } from "./free-port.js";

const STARTUP_TIMEOUT_MS = 15_000;
const POLL_INTERVAL_MS = 500;

type ManagedProcess = {
  name: string;
  child: ChildProcess;
  logPath: string;
  logStream: ReturnType<typeof createWriteStream>;
};

function resolvePaths() {
  const harnessDir = path.dirname(fileURLToPath(import.meta.url));
  const frontendDir = path.resolve(harnessDir, "..", "..");
  const repoRoot = path.resolve(frontendDir, "..");
  const backendDir = path.join(repoRoot, "backend");
  return { harnessDir, frontendDir, repoRoot, backendDir };
}

function delay(ms: number) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

async function waitForUrl(url: string, timeoutMs: number) {
  const deadline = Date.now() + timeoutMs;
  let lastError: unknown;

  while (Date.now() < deadline) {
    try {
      const controller = new AbortController();
      const timer = setTimeout(() => controller.abort(), 2_000);
      const response = await fetch(url, { signal: controller.signal });
      clearTimeout(timer);
      if (response.ok || response.status < 500) {
        return;
      }
      lastError = new Error(`unexpected status ${response.status}`);
    } catch (error) {
      lastError = error;
    }
    await delay(POLL_INTERVAL_MS);
  }

  const detail = lastError instanceof Error ? lastError.message : String(lastError ?? "unknown error");
  throw new Error(`Timed out waiting for ${url}: ${detail}`);
}

function startProcess(
  name: string,
  command: string,
  args: string[],
  options: {
    cwd: string;
    env: NodeJS.ProcessEnv;
    logPath: string;
  },
): ManagedProcess {
  const child = spawn(command, args, {
    cwd: options.cwd,
    env: options.env,
    stdio: ["ignore", "pipe", "pipe"],
  });

  const logStream = createWriteStream(options.logPath, { flags: "a" });
  child.stdout?.pipe(logStream);
  child.stderr?.pipe(logStream);

  return {
    name,
    child,
    logPath: options.logPath,
    logStream,
  };
}

async function stopProcess(proc: ManagedProcess) {
  if (proc.child.exitCode !== null || proc.child.killed) {
    proc.logStream.end();
    return;
  }

  proc.child.kill("SIGTERM");

  const exitPromise = new Promise<void>((resolve) => {
    proc.child.once("exit", () => resolve());
  });

  const timeout = delay(5_000).then(() => {
    if (proc.child.exitCode === null && !proc.child.killed) {
      proc.child.kill("SIGKILL");
    }
  });

  await Promise.race([exitPromise, timeout]);
  proc.logStream.end();
}

/** Build a Go binary under backendDir/cmd/<name> and return its path. */
async function buildGoBinary(
  name: string,
  backendDir: string,
  outDir: string,
): Promise<string> {
  const outPath = path.join(outDir, name);
  const result = spawnSync("go", ["build", "-o", outPath, `./cmd/${name}`], {
    cwd: backendDir,
    encoding: "utf-8",
  });
  if (result.status !== 0) {
    throw new Error(
      `Failed to build ${name}:\nstdout: ${result.stdout}\nstderr: ${result.stderr}`,
    );
  }
  return outPath;
}

export default async function globalSetup() {
  const { frontendDir, repoRoot, backendDir } = resolvePaths();
  // reservePort reads from process.env if already set (e.g. from the config
  // file loading first), otherwise allocates a fresh free port and writes it
  // back to process.env so both config and harness agree on the same value.
  const backendPort = reservePort("E2E_BACKEND_PORT", 8090);
  const frontendPort = reservePort("E2E_FRONTEND_PORT", 4174);
  const logDir = path.join(frontendDir, "test-results", "e2e-logs");

  await fs.mkdir(logDir, { recursive: true });
  const stateDir = await fs.mkdtemp(path.join(os.tmpdir(), "orbitmesh-e2e-"));

  // Build helper binaries used by e2e tests and expose paths as env vars.
  const acpEchoBin = await buildGoBinary("acp-echo", backendDir, stateDir);
  process.env.ACP_ECHO_BIN = acpEchoBin;

  // Expose the backend URL so test helpers can make direct HTTP requests
  // (bypassing the browser's connection-per-origin limit).
  process.env.E2E_BACKEND_URL = `http://127.0.0.1:${backendPort}`;

  const backendLog = path.join(logDir, "backend.log");
  const frontendLog = path.join(logDir, "frontend.log");

  let backend: ManagedProcess | null = null;
  let frontend: ManagedProcess | null = null;

  try {
    backend = startProcess(
      "backend",
      "go",
      ["run", "./cmd/orbitmesh"],
      {
        cwd: backendDir,
        env: {
          ...process.env,
          ORBITMESH_BASE_DIR: stateDir,
          ORBITMESH_GIT_DIR: repoRoot,
          ORBITMESH_ENV: "test",
          E2E_BACKEND_PORT: String(backendPort),
        },
        logPath: backendLog,
      },
    );

    frontend = startProcess(
      "frontend",
      "npm",
      ["run", "dev", "--", "--host", "127.0.0.1", "--port", String(frontendPort)],
      {
        cwd: frontendDir,
        env: {
          ...process.env,
          E2E_FRONTEND_PORT: String(frontendPort),
        },
        logPath: frontendLog,
      },
    );

    await waitForUrl(
      `http://127.0.0.1:${backendPort}/api/v1/me/permissions`,
      STARTUP_TIMEOUT_MS,
    );
    await waitForUrl(`http://127.0.0.1:${frontendPort}`, STARTUP_TIMEOUT_MS);
  } catch (error) {
    if (frontend) {
      await stopProcess(frontend);
    }
    if (backend) {
      await stopProcess(backend);
    }
    throw error;
  }

  return async () => {
    if (frontend) {
      await stopProcess(frontend);
    }
    if (backend) {
      await stopProcess(backend);
    }
  };
}
