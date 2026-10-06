// Packaging is native-only until each cross-platform installer route is qualified.
// Refusing a mismatched binary is preferable to silently shipping it in an installer.
const requested = process.argv[2];
const platform = {linux:'linux',win32:'windows',darwin:'darwin'}[process.platform];
const arch = {x64:'amd64',arm64:'arm64'}[process.arch];
if (!arch || requested!==platform || (process.env.GOOS && process.env.GOOS!==platform) || (process.env.GOARCH && process.env.GOARCH!==arch)) {
  console.error(`Packaging requires a native ${requested} x64/ARM64 environment with matching GOOS/GOARCH. Current: ${platform}/${arch}.`);
  process.exit(1);
}
console.log(`Packaging target verified: ${platform}/${arch}`);
