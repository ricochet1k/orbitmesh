import { existsSync, readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";

const scriptDir = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(scriptDir, "..", "..");
const generatedPath = resolve(repoRoot, "frontend", "src", "types", "generated", "realtime.ts");

const fail = (message) => {
  process.stderr.write(`${message}\n`);
  process.exit(1);
};

if (!existsSync(generatedPath)) {
  fail("Missing frontend/src/types/generated/realtime.ts. Run `pnpm --dir frontend run generate:realtime-types`.");
}

const hasGo = spawnSync("go", ["version"], { stdio: "ignore" }).status === 0;
if (!hasGo) {
  fail(
    "Go is required to verify frontend realtime type generation. Install Go and run `pnpm --dir frontend run check:realtime-types`.",
  );
}

const before = readFileSync(generatedPath, "utf8");

const generate = spawnSync("go", ["run", "./backend/cmd/typegen-realtime"], {
  cwd: repoRoot,
  stdio: "inherit",
});
if (generate.status !== 0) {
  process.exit(generate.status ?? 1);
}

const after = readFileSync(generatedPath, "utf8");
if (before !== after) {
  fail(
    "frontend/src/types/generated/realtime.ts is out of sync. Run `pnpm --dir frontend run generate:realtime-types` and commit the result.",
  );
}

process.stdout.write("realtime.ts is in sync\n");
