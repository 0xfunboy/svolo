// pi extension: `pi --svolo` opens svolo in the current directory instead of the terminal UI.
// Installed through this repo's package.json `pi` manifest (`pi install <path to this checkout>`).
// It runs the installed app (/Applications or ~/Applications) when there is one, else this checkout's build
// (SVOLO_DEV=1 forces the checkout). Either way it hands the terminal to svolo (which keeps logging there)
// and exits pi with svolo's exit code.
// In every other pi process, including svolo's own `pi --mode rpc` children, it only registers the flag.
import { spawn } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import { homedir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";

const here = typeof __dirname === "string" ? __dirname : dirname(fileURLToPath(import.meta.url));
const root = join(here, "..");

function installedApp(productName: string): string | undefined {
  if (process.env.SVOLO_DEV === "1" || process.platform !== "darwin") return undefined;
  return ["/Applications", join(homedir(), "Applications")]
    .map((dir) => join(dir, `${productName}.app`, "Contents", "MacOS", productName))
    .find((binary) => existsSync(binary));
}

export default async function (pi: ExtensionAPI) {
  pi.registerFlag("svolo", { type: "boolean", description: "Open svolo (desktop app) in this directory instead of the terminal UI" });
  // Flag values are not available to factories yet, and the hand-off must happen before the TUI starts.
  if (!process.argv.slice(2).includes("--svolo")) return;

  const { productName } = JSON.parse(readFileSync(join(root, "package.json"), "utf8")) as { productName: string };
  const app = installedApp(productName);
  if (!app && !existsSync(join(root, "out", "main", "index.js"))) {
    console.error(`pi --svolo: build ${productName} from this checkout (pnpm install && pnpm build in ${root}).`);
    process.exit(1);
  }
  const [command, args] = app ? [app, []] : [process.execPath, [join(root, "bin", "svolo.mjs")]];
  const env: NodeJS.ProcessEnv = { ...process.env, SVOLO_CWD: process.cwd() };
  delete env.ELECTRON_RUN_AS_NODE;
  const child = spawn(command, args, { cwd: process.cwd(), stdio: "inherit", env });
  // Stay alive until svolo has shut its pi sessions down; pass signals on (svolo's quit is idempotent).
  for (const signal of ["SIGINT", "SIGTERM", "SIGHUP"] as const) process.on(signal, () => child.kill(signal));
  const code = await new Promise<number>((resolve) => {
    child.on("exit", (exitCode, signal) => resolve(exitCode ?? (signal ? 1 : 0)));
    child.on("error", (error) => {
      console.error(`pi --svolo: ${error.message}`);
      resolve(1);
    });
  });
  process.exit(code);
}
