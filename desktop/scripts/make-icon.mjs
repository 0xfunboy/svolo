// Compatibility entrypoint for scripts that invoke the icon task directly.
// The brand pipeline generates all web and desktop sizes from approved sources.
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
const script = fileURLToPath(new URL("../../scripts/build-brand.py", import.meta.url));
const executable = process.env.PYTHON || (process.platform === "win32" ? "python" : "python3");
const result = spawnSync(executable, [script], { stdio: "inherit" });
if (result.error) { console.error(result.error.message); process.exit(1); }
process.exit(result.status ?? 1);
