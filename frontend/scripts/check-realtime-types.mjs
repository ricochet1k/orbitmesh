import { existsSync, readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";

const scriptDir = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(scriptDir, "..", "..");
const generatedRealtimePath = resolve(repoRoot, "frontend", "src", "types", "generated", "realtime.ts");
const generatedTranscriptPath = resolve(repoRoot, "frontend", "src", "types", "generated", "transcript.ts");

const fail = (message) => {
  process.stderr.write(`${message}\n`);
  process.exit(1);
};

if (!existsSync(generatedRealtimePath) || !existsSync(generatedTranscriptPath)) {
  fail("Missing frontend/src/types/generated/realtime.ts or transcript.ts. Run `pnpm --dir frontend run generate:realtime-types`.");
}

const hasGo = spawnSync("go", ["version"], { stdio: "ignore" }).status === 0;
if (!hasGo) {
  fail(
    "Go is required to verify frontend realtime type generation. Install Go and run `pnpm --dir frontend run check:realtime-types`.",
  );
}

const beforeRealtime = readFileSync(generatedRealtimePath, "utf8");
const beforeTranscript = readFileSync(generatedTranscriptPath, "utf8");

const generate = spawnSync("go", ["run", "./cmd/typegen-realtime"], {
  cwd: resolve(repoRoot, "backend"),
  stdio: "inherit",
});
if (generate.status !== 0) {
  process.exit(generate.status ?? 1);
}

const afterRealtime = readFileSync(generatedRealtimePath, "utf8");
const afterTranscript = readFileSync(generatedTranscriptPath, "utf8");
if (beforeRealtime !== afterRealtime || beforeTranscript !== afterTranscript) {
  fail(
    "Generated realtime/transcript types are out of sync. Run `pnpm --dir frontend run generate:realtime-types` and commit the result.",
  );
}

process.stdout.write("realtime.ts and transcript.ts are in sync\n");
