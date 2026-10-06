import { readFileSync } from "node:fs";
const product = JSON.parse(readFileSync(new URL("../../product.json", import.meta.url), "utf8"));
if (!product.productionQualified || product.releaseChannel !== "stable") {
  throw new Error("Publishing is disabled: complete docs/RELEASE.md and configure a verified publisher channel.");
}
throw new Error("No publishing destination is configured. Source verification never publishes artifacts.");
