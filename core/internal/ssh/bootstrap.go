package ssh

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"svolo.local/core/internal/processenv"
	"svolo.local/core/internal/service"
)

type Platform struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
}
type BootstrapOptions struct {
	Platform Platform `json:"platform"`
	Mode     string   `json:"mode"`
	Replace  bool     `json:"replace"`
}
type BootstrapResult struct {
	Host          string                `json:"host"`
	Platform      Platform              `json:"platform"`
	SHA256        string                `json:"sha256"`
	Installation  service.InstallResult `json:"installation"`
	CredentialRef string                `json:"credentialRef"`
}

func CommandArgs(alias, command string) []string {
	return []string{"-T", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes", "-o", "ForwardAgent=no", "-o", "ConnectTimeout=10", "--", alias, command}
}
func (m *Manager) run(ctx context.Context, h Host, command string, in io.Reader) ([]byte, error) {
	if !aliasRE.MatchString(h.Alias) {
		return nil, errors.New("invalid SSH alias")
	}
	cmd := exec.CommandContext(ctx, m.Binary, CommandArgs(h.Alias, command)...)
	cmd.Stdin = in
	cmd.Env = processenv.SSH()
	out := &capture{max: 2 << 20}
	tail := &limited{}
	cmd.Stdout = out
	cmd.Stderr = tail
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("SSH remote operation: %w: %s", err, tail.String())
	}
	if out.overflow {
		return nil, errors.New("remote response exceeds budget")
	}
	return out.b, nil
}

type capture struct {
	b        []byte
	max      int
	overflow bool
}

func (w *capture) Write(p []byte) (int, error) {
	n := len(p)
	remaining := w.max - len(w.b)
	if n > remaining {
		p = p[:remaining]
		w.overflow = true
	}
	w.b = append(w.b, p...)
	return n, nil
}
func normalizePlatform(osName, arch string) (Platform, error) {
	p := Platform{strings.ToLower(osName), strings.ToLower(arch)}
	switch p.OS {
	case "linux", "darwin", "windows":
	default:
		return p, errors.New("remote OS must be Linux, Windows or macOS")
	}
	switch p.Arch {
	case "x86_64", "x64", "amd64":
		p.Arch = "amd64"
	case "aarch64", "arm64":
		p.Arch = "arm64"
	default:
		return p, errors.New("remote architecture must be amd64 or arm64")
	}
	return p, nil
}
func (m *Manager) Detect(ctx context.Context, h Host) (Platform, error) {
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	b, err := m.run(ctx, h, "uname -sm", nil)
	if err == nil {
		parts := strings.Fields(string(b))
		if len(parts) == 2 {
			return normalizePlatform(parts[0], parts[1])
		}
	}
	b, err = m.run(ctx, h, powershell(`Write-Output ('windows ' + $env:PROCESSOR_ARCHITECTURE)`), nil)
	if err != nil {
		return Platform{}, fmt.Errorf("could not detect remote OS using POSIX or PowerShell: %w", err)
	}
	parts := strings.Fields(strings.TrimPrefix(string(b), "\ufeff"))
	if len(parts) != 2 {
		return Platform{}, errors.New("ambiguous remote platform response")
	}
	return normalizePlatform(parts[0], parts[1])
}
func LocateArtifact(root string, p Platform) (string, error) {
	p, err := normalizePlatform(p.OS, p.Arch)
	if err != nil {
		return "", err
	}
	exe := "svolo-core"
	if p.OS == "windows" {
		exe += ".exe"
	}
	candidates := []string{}
	if root != "" {
		candidates = append(candidates, filepath.Join(root, p.OS+"-"+p.Arch, exe))
	}
	current, e := os.Executable()
	if e == nil {
		if p.OS == runtime.GOOS && p.Arch == runtime.GOARCH {
			candidates = append(candidates, current)
		}
		candidates = append(candidates, filepath.Join(filepath.Dir(current), "platforms", p.OS+"-"+p.Arch, exe), filepath.Join(filepath.Dir(current), "..", p.OS+"-"+p.Arch, exe))
	}
	for _, path := range candidates {
		if info, e := os.Stat(path); e == nil && info.Mode().IsRegular() && info.Size() > 0 && info.Size() < 128<<20 {
			return path, nil
		}
	}
	return "", fmt.Errorf("no verified local build for %s/%s; build core artifacts and configure --artifacts-dir", p.OS, p.Arch)
}
func (m *Manager) Bootstrap(ctx context.Context, h Host, o BootstrapOptions, binaryPath string, saveToken func(string, string) error) (BootstrapResult, error) {
	result := BootstrapResult{Host: h.ID, Platform: o.Platform, CredentialRef: "ssh-" + h.ID}
	if err := Validate(h); err != nil {
		return result, err
	}
	if saveToken == nil {
		return result, errors.New("encrypted credential persistence is required before bootstrap")
	}
	if o.Mode != "process" && o.Mode != "user-service" {
		return result, errors.New("bootstrap mode must be process or user-service")
	}
	p, err := normalizePlatform(o.Platform.OS, o.Platform.Arch)
	if err != nil {
		return result, err
	}
	result.Platform = p
	f, err := os.Open(binaryPath)
	if err != nil {
		return result, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() > 128<<20 {
		return result, errors.New("invalid core binary artifact")
	}
	hash := sha256.New()
	if _, err = io.Copy(hash, f); err != nil {
		return result, err
	}
	if _, err = f.Seek(0, 0); err != nil {
		return result, err
	}
	result.SHA256 = hex.EncodeToString(hash.Sum(nil))
	command, err := BootstrapCommand(h, p, result.SHA256, o.Mode, o.Replace)
	if err != nil {
		return result, err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	output, err := m.run(ctx, h, command, f)
	if err != nil {
		return result, err
	}
	var response struct {
		Result service.InstallResult `json:"result"`
		Token  string                `json:"token"`
	}
	if err = json.Unmarshal(output, &response); err != nil {
		return result, errors.New("remote installer returned an invalid private response (not logged)")
	}
	if len(response.Token) < 32 || len(response.Token) > 256 {
		return result, errors.New("invalid remote daemon credential")
	}
	if response.Result.Info.Version == "" || !strings.HasPrefix(response.Result.Info.URL, "http://127.0.0.1:") {
		return result, errors.New("invalid remote daemon identity")
	}
	if err = saveToken(result.CredentialRef, response.Token); err != nil {
		return result, fmt.Errorf("daemon installed but encrypted credential persistence failed: %w", err)
	}
	result.Installation = response.Result
	return result, nil
}
func powershell(script string) string {
	u := utf16.Encode([]rune(script))
	b := make([]byte, len(u)*2)
	for i, v := range u {
		binary.LittleEndian.PutUint16(b[i*2:], v)
	}
	return "powershell.exe -NoLogo -NoProfile -NonInteractive -EncodedCommand " + base64.StdEncoding.EncodeToString(b)
}
func shQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
func BootstrapCommand(h Host, p Platform, sha, mode string, replace bool) (string, error) {
	if err := Validate(h); err != nil {
		return "", err
	}
	if !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(sha) {
		return "", errors.New("invalid artifact hash")
	}
	if mode != "process" && mode != "user-service" {
		return "", errors.New("invalid service mode")
	}
	if _, err := normalizePlatform(p.OS, p.Arch); err != nil {
		return "", err
	}
	suffix := ""
	if replace {
		suffix = " --replace"
	}
	if p.OS == "windows" {
		script := `$ErrorActionPreference='Stop';[Console]::OutputEncoding=[System.Text.UTF8Encoding]::new($false);` +
			`$root=Join-Path $env:LOCALAPPDATA 'svolo-agent-browser\hosts\` + h.ID + `';New-Item -ItemType Directory -Force -Path $root|Out-Null;` +
			`$temp=Join-Path $root ([System.IO.Path]::GetRandomFileName());$file=[System.IO.File]::Open($temp,[System.IO.FileMode]::CreateNew);` +
			`try{[Console]::OpenStandardInput().CopyTo($file)}finally{$file.Dispose()};` +
			`try{if((Get-FileHash -Algorithm SHA256 -LiteralPath $temp).Hash.ToLower() -ne '` + sha + `'){throw 'binary checksum mismatch'};` +
			`$bin=Join-Path $root 'svolo-core-` + sha + `.exe';if(!(Test-Path -LiteralPath $bin)){Move-Item -LiteralPath $temp -Destination $bin};` +
			`& $bin service install --data (Join-Path $root 'data') --port ` + strconv.Itoa(h.RemotePort) + ` --mode ` + mode + ` --reply-credentials` + suffix + `;if($LASTEXITCODE -ne 0){throw 'service installer failed'}` +
			`}finally{Remove-Item -LiteralPath $temp -Force -ErrorAction SilentlyContinue}`
		return powershell(script), nil
	}
	checksum := "sha256sum"
	if p.OS == "darwin" {
		checksum = "shasum -a 256"
	}
	script := `set -eu; umask 077; root="$HOME/.local/share/svolo-agent-browser/hosts/` + h.ID + `"; mkdir -p "$root"; test ! -L "$root"; tmp=$(mktemp "$root/.incoming.XXXXXXXX"); trap 'rm -f "$tmp"' EXIT HUP INT TERM; cat > "$tmp"; actual=$(` + checksum + ` "$tmp"); actual=${actual%% *}; test "$actual" = ` + shQuote(sha) + ` || { echo 'binary checksum mismatch' >&2; exit 1; }; bin="$root/svolo-core-` + sha + `"; if test ! -f "$bin"; then chmod 700 "$tmp"; mv "$tmp" "$bin"; fi; "$bin" service install --data "$root/data" --port ` + strconv.Itoa(h.RemotePort) + ` --mode ` + mode + ` --reply-credentials` + suffix
	return "sh -c " + shQuote(script), nil
}
