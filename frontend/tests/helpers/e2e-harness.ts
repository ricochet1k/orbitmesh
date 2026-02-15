import { spawn, type ChildProcess } from "node:child_process";
import { createWriteStream, promises as fs } from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

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

export default async function globalSetup() {
  const { frontendDir, repoRoot, backendDir } = resolvePaths();
  const backendPort = Number(process.env.E2E_BACKEND_PORT ?? "8080");
  const frontendPort = Number(process.env.E2E_FRONTEND_PORT ?? "4173");
  const logDir = path.join(frontendDir, "test-results", "e2e-logs");

  await fs.mkdir(logDir, { recursive: true });
  const stateDir = await fs.mkdtemp(path.join(os.tmpdir(), "orbitmesh-e2e-"));

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
