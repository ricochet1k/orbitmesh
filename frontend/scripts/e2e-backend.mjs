import { spawn } from "node:child_process";
import { mkdirSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const frontendDir = path.resolve(scriptDir, "..");
const repoRoot = path.resolve(frontendDir, "..");
const backendDir = path.join(repoRoot, "backend");
const baseDir = process.env.ORBITMESH_E2E_BASE_DIR || path.join(repoRoot, "tests", ".orbitmesh-e2e");

mkdirSync(baseDir, { recursive: true });

const child = spawn("go", ["run", "./cmd/orbitmesh"], {
  cwd: backendDir,
  env: {
    ...process.env,
    ORBITMESH_BASE_DIR: baseDir,
    ORBITMESH_GIT_DIR: repoRoot,
    ORBITMESH_ENV: process.env.ORBITMESH_ENV || "test",
  },
  stdio: "inherit",
});

child.on("exit", (code, signal) => {
  if (signal) {
    process.kill(process.pid, signal);
    return;
  }
  process.exitCode = code ?? 1;
});
