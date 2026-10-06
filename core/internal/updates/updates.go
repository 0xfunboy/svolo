// Package updates accepts only publisher-signed release manifests from explicitly
// configured HTTPS origins. It stages verified files; it never executes a shell
// or silently replaces a running application. OS installer verification remains
// a separate deployment gate, not something an Ed25519 signature can replace.
package updates

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"svolo.local/core/internal/store"
	"sync"
	"time"
)

const MaxArtifact = int64(2 << 30)
const MaxManifest = 1 << 20

type Trust struct {
	Feed         string            `json:"feed"`
	Channel      string            `json:"channel"`
	Keys         map[string]string `json:"keys"`
	AllowedHosts []string          `json:"allowedHosts"`
}
type Artifact struct {
	OS     string `json:"os"`
	Arch   string `json:"arch"`
	Kind   string `json:"kind"`
	URL    string `json:"url"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}
type Manifest struct {
	Product   string     `json:"product"`
	Channel   string     `json:"channel"`
	Version   string     `json:"version"`
	Sequence  uint64     `json:"sequence"`
	Published time.Time  `json:"published"`
	Expires   time.Time  `json:"expires"`
	Notes     string     `json:"notes"`
	Artifacts []Artifact `json:"artifacts"`
}
type Envelope struct {
	KeyID     string `json:"keyId"`
	Payload   string `json:"payload"`
	Signature string `json:"signature"`
}
type Seen struct {
	Sequence uint64 `json:"sequence"`
	Digest   string `json:"digest"`
}
type Status struct {
	Configured              bool      `json:"configured"`
	Current                 string    `json:"current"`
	Phase                   string    `json:"phase"`
	Detail                  string    `json:"detail,omitempty"`
	Release                 *Manifest `json:"release,omitempty"`
	Artifact                *Artifact `json:"artifact,omitempty"`
	Staged                  string    `json:"staged,omitempty"`
	SignatureVerified       bool      `json:"signatureVerified"`
	NativeSignatureVerified bool      `json:"nativeSignatureVerified"`
}
type Manager struct {
	mu                        sync.Mutex
	root, current, goos, arch string
	trust                     *Trust
	client                    *http.Client
	now                       func() time.Time
	seen                      Seen
	state                     Status
	envelope                  *Envelope
}

func New(root, current, trustFile string) (*Manager, error) {
	m := &Manager{root: root, current: current, goos: runtime.GOOS, arch: runtime.GOARCH, now: time.Now,
		client: &http.Client{Timeout: 20 * time.Minute}, state: Status{Current: current, Phase: "disabled", Detail: "No publisher trust file configured. No update authority is assumed."}}
	if trustFile == "" {
		return m, nil
	}
	f, e := os.Open(trustFile)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	raw, e := io.ReadAll(io.LimitReader(f, 65537))
	if e != nil || len(raw) > 65536 {
		return nil, errors.New("update trust file exceeds budget")
	}
	var t Trust
	if e = strict(raw, &t); e != nil {
		return nil, fmt.Errorf("invalid update trust file: %w", e)
	}
	if e = t.Validate(); e != nil {
		return nil, e
	}
	m.trust = &t
	m.client.CheckRedirect = func(r *http.Request, via []*http.Request) error {
		if len(via) > 3 {
			return errors.New("update redirect limit")
		}
		return t.allowed(r.URL.String())
	}
	seenRaw, e := os.ReadFile(filepath.Join(root, "updates-seen.json"))
	if e == nil {
		if e = strict(seenRaw, &m.seen); e != nil {
			return nil, fmt.Errorf("update rollback ledger is corrupt: %w", e)
		}
	} else if !os.IsNotExist(e) {
		return nil, e
	}
	m.state = Status{Configured: true, Current: current, Phase: "idle"}
	return m, nil
}
func strict(raw []byte, dst any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if e := d.Decode(dst); e != nil {
		return e
	}
	var extra any
	if e := d.Decode(&extra); e != io.EOF {
		return errors.New("trailing JSON data")
	}
	return nil
}
func (t Trust) Validate() error {
	if len(t.Keys) < 1 || len(t.Keys) > 8 || len(t.AllowedHosts) < 1 || len(t.AllowedHosts) > 16 {
		return errors.New("update trust requires 1..8 public keys and 1..16 exact HTTPS hosts")
	}
	if t.Channel != "stable" && t.Channel != "preview" {
		return errors.New("update channel must be stable or preview")
	}
	for id, key := range t.Keys {
		if !store.ValidID(id) {
			return errors.New("invalid publisher key id")
		}
		b, e := base64.StdEncoding.DecodeString(key)
		if e != nil || len(b) != ed25519.PublicKeySize {
			return errors.New("invalid Ed25519 publisher public key")
		}
	}
	for _, h := range t.AllowedHosts {
		u, e := url.Parse("https://" + h)
		if e != nil || u.Host != h || u.Path != "" || strings.ContainsAny(h, "@*?#/\\") || h == "" {
			return errors.New("update hosts must be exact host[:port], not URLs/wildcards")
		}
	}
	return t.allowed(t.Feed)
}
func (t Trust) allowed(raw string) error {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" {
		return errors.New("updates require HTTPS without URL userinfo/fragments")
	}
	for _, h := range t.AllowedHosts {
		if strings.EqualFold(u.Host, h) {
			return nil
		}
	}
	return errors.New("update URL is outside configured publisher hosts")
}

var semver = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z.-]+))?(?:\+[0-9A-Za-z.-]+)?$`)

func Compare(a, b string) (int, error) {
	aa, bb := semver.FindStringSubmatch(a), semver.FindStringSubmatch(b)
	if aa == nil || bb == nil {
		return 0, errors.New("invalid semantic version")
	}
	for i := 1; i <= 3; i++ {
		x, e := strconv.ParseUint(aa[i], 10, 64)
		if e != nil {
			return 0, e
		}
		y, e := strconv.ParseUint(bb[i], 10, 64)
		if e != nil {
			return 0, e
		}
		if x < y {
			return -1, nil
		}
		if x > y {
			return 1, nil
		}
	}
	if aa[4] == bb[4] {
		return 0, nil
	}
	if aa[4] == "" {
		return 1, nil
	}
	if bb[4] == "" {
		return -1, nil
	}
	x, y := strings.Split(aa[4], "."), strings.Split(bb[4], ".")
	for i := 0; i < len(x) && i < len(y); i++ {
		if x[i] == y[i] {
			continue
		}
		xn, xe := strconv.ParseUint(x[i], 10, 64)
		yn, ye := strconv.ParseUint(y[i], 10, 64)
		if xe == nil && ye == nil {
			if xn < yn {
				return -1, nil
			}
			return 1, nil
		}
		if xe == nil {
			return -1, nil
		}
		if ye == nil {
			return 1, nil
		}
		if x[i] < y[i] {
			return -1, nil
		}
		return 1, nil
	}
	if len(x) < len(y) {
		return -1, nil
	}
	return 1, nil
}
func (m *Manager) verify(en Envelope) (Manifest, Seen, error) {
	var out Manifest
	empty := Seen{}
	if m.trust == nil {
		return out, empty, errors.New("publisher trust is not configured")
	}
	pub, ok := m.trust.Keys[en.KeyID]
	if !ok {
		return out, empty, errors.New("untrusted publisher key")
	}
	key, _ := base64.StdEncoding.DecodeString(pub)
	payload, e := base64.StdEncoding.DecodeString(en.Payload)
	if e != nil || len(payload) > MaxManifest/2 {
		return out, empty, errors.New("invalid signed payload")
	}
	sig, e := base64.StdEncoding.DecodeString(en.Signature)
	if e != nil || !ed25519.Verify(key, payload, sig) {
		return out, empty, errors.New("publisher signature verification failed")
	}
	if e = strict(payload, &out); e != nil {
		return out, empty, e
	}
	if out.Product != "svolo" || out.Channel != m.trust.Channel || out.Sequence == 0 {
		return out, empty, errors.New("release identity/channel/sequence mismatch")
	}
	if _, e = Compare(out.Version, m.current); e != nil {
		return out, empty, e
	}
	now := m.now()
	if out.Published.IsZero() || out.Published.After(now.Add(5*time.Minute)) || !out.Expires.After(now) || !out.Expires.After(out.Published) || out.Expires.Sub(out.Published) > 31*24*time.Hour {
		return out, empty, errors.New("release metadata expired or outside validity window")
	}
	if out.Channel == "stable" && strings.Contains(strings.Split(out.Version, "+")[0], "-") {
		return out, empty, errors.New("prerelease on stable channel")
	}
	if len(out.Notes) > 64<<10 || len(out.Artifacts) < 1 || len(out.Artifacts) > 24 {
		return out, empty, errors.New("release manifest exceeds content budget")
	}
	targets := map[string]bool{}
	for _, a := range out.Artifacts {
		if (a.OS != "linux" && a.OS != "windows" && a.OS != "darwin") || (a.Arch != "amd64" && a.Arch != "arm64") {
			return out, empty, errors.New("unsupported artifact platform")
		}
		if a.Kind != "deb" && a.Kind != "AppImage" && a.Kind != "exe" && a.Kind != "dmg" && a.Kind != "zip" {
			return out, empty, errors.New("unsupported installer kind")
		}
		if (a.OS == "linux" && a.Kind != "deb" && a.Kind != "AppImage" && a.Kind != "zip") || (a.OS == "windows" && a.Kind != "exe" && a.Kind != "zip") || (a.OS == "darwin" && a.Kind != "dmg" && a.Kind != "zip") {
			return out, empty, errors.New("installer kind does not match platform")
		}
		if a.Size <= 0 || a.Size > MaxArtifact || len(a.SHA256) != 64 {
			return out, empty, errors.New("invalid artifact size/hash")
		}
		hash, e := hex.DecodeString(a.SHA256)
		if e != nil || len(hash) != 32 {
			return out, empty, errors.New("invalid artifact hash")
		}
		if e = m.trust.allowed(a.URL); e != nil {
			return out, empty, e
		}
		target := a.OS + "/" + a.Arch
		if targets[target] {
			return out, empty, errors.New("duplicate release target")
		}
		targets[target] = true
	}
	sum := sha256.Sum256(payload)
	seen := Seen{Sequence: out.Sequence, Digest: hex.EncodeToString(sum[:])}
	if seen.Sequence < m.seen.Sequence || (seen.Sequence == m.seen.Sequence && seen.Digest != m.seen.Digest) {
		return out, empty, errors.New("release rollback/equivocation rejected")
	}
	return out, seen, nil
}
func (m *Manager) Status() Status { m.mu.Lock(); defer m.mu.Unlock(); return clone(m.state) }
func clone(s Status) Status {
	b, _ := json.Marshal(s)
	var c Status
	_ = json.Unmarshal(b, &c)
	return c
}
func (m *Manager) Check(ctx context.Context) (Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.trust == nil {
		return clone(m.state), errors.New(m.state.Detail)
	}
	req, e := http.NewRequestWithContext(ctx, "GET", m.trust.Feed, nil)
	if e != nil {
		return clone(m.state), e
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Svolo/"+m.current)
	res, e := m.client.Do(req)
	if e != nil {
		return clone(m.state), errors.New("publisher update feed could not be reached")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return clone(m.state), fmt.Errorf("publisher feed HTTP %d", res.StatusCode)
	}
	raw, e := io.ReadAll(io.LimitReader(res.Body, MaxManifest+1))
	if e != nil || len(raw) > MaxManifest {
		return clone(m.state), errors.New("publisher feed exceeds size budget")
	}
	var en Envelope
	if e = strict(raw, &en); e != nil {
		return clone(m.state), e
	}
	manifest, seen, e := m.verify(en)
	if e != nil {
		return clone(m.state), e
	}
	var artifact *Artifact
	for _, a := range manifest.Artifacts {
		if a.OS == m.goos && a.Arch == m.arch {
			v := a
			artifact = &v
			break
		}
	}
	if artifact == nil {
		return clone(m.state), errors.New("release has no installer for this host platform")
	}
	ledger, _ := json.Marshal(seen)
	if e = store.Atomic(filepath.Join(m.root, "updates-seen.json"), ledger, 0600); e != nil {
		return clone(m.state), e
	}
	m.seen = seen
	cmp, e := Compare(manifest.Version, m.current)
	if e != nil {
		return clone(m.state), e
	}
	if cmp <= 0 {
		m.state = Status{Configured: true, Current: m.current, Phase: "idle", SignatureVerified: true}
		m.envelope = nil
		return clone(m.state), nil
	}
	old := m.state
	m.state = Status{Configured: true, Current: m.current, Phase: "available", Release: &manifest, Artifact: artifact, SignatureVerified: true, Detail: "Publisher signature verified. Installer must still pass native OS verification before installation."}
	if old.Staged != "" && old.Artifact != nil && old.Artifact.SHA256 == artifact.SHA256 {
		m.state.Staged = old.Staged
		m.state.Phase = "staged"
	}
	m.envelope = &en
	return clone(m.state), nil
}
func (m *Manager) Stage(ctx context.Context) (Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.envelope == nil || m.state.Artifact == nil {
		return clone(m.state), errors.New("check a signed release first")
	}
	if _, _, e := m.verify(*m.envelope); e != nil {
		return clone(m.state), e
	}
	a := *m.state.Artifact
	dir := filepath.Join(m.root, "updates")
	if e := os.MkdirAll(dir, 0700); e != nil {
		return clone(m.state), e
	}
	staged := filepath.Join(dir, "svolo-"+m.state.Release.Version+"-"+a.OS+"-"+a.Arch+"."+a.Kind)
	if m.state.Staged == staged {
		if e := verifyFile(staged, a); e == nil {
			return clone(m.state), nil
		}
		m.state.Staged = ""
		m.state.Phase = "available"
	}
	f, e := os.CreateTemp(dir, ".download-")
	if e != nil {
		return clone(m.state), e
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	defer f.Close()
	req, e := http.NewRequestWithContext(ctx, "GET", a.URL, nil)
	if e != nil {
		return clone(m.state), e
	}
	req.Header.Set("Accept-Encoding", "identity")
	res, e := m.client.Do(req)
	if e != nil {
		return clone(m.state), errors.New("installer download could not be reached")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 || res.Uncompressed || (res.ContentLength >= 0 && res.ContentLength != a.Size) {
		return clone(m.state), errors.New("installer response size/status/encoding mismatch")
	}
	h := sha256.New()
	n, e := io.Copy(io.MultiWriter(f, h), io.LimitReader(res.Body, a.Size))
	if e != nil {
		return clone(m.state), e
	}
	extra := make([]byte, 1)
	k, tailErr := res.Body.Read(extra)
	if n != a.Size || k != 0 || (tailErr != nil && tailErr != io.EOF) || !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), a.SHA256) {
		return clone(m.state), errors.New("installer length/hash mismatch")
	}
	if e = f.Sync(); e != nil {
		return clone(m.state), e
	}
	if e = f.Close(); e != nil {
		return clone(m.state), e
	}
	if _, e = os.Lstat(staged); e == nil {
		if e = verifyFile(staged, a); e != nil {
			return clone(m.state), errors.New("staging destination already exists with different content")
		}
		os.Remove(tmp)
	} else if os.IsNotExist(e) {
		if e = os.Rename(tmp, staged); e != nil {
			return clone(m.state), e
		}
	} else {
		return clone(m.state), e
	}
	if previous := m.state.Staged; previous != "" && previous != staged && filepath.Dir(previous) == dir {
		_ = os.Remove(previous)
	}
	m.state.Staged = staged
	m.state.Phase = "staged"
	m.state.Detail = "Downloaded file matches the signed release manifest. Native OS signature is NOT yet verified; use the signed production installer workflow. No automatic self-replacement occurred."
	return clone(m.state), nil
}
func verifyFile(path string, a Artifact) error {
	st, e := os.Lstat(path)
	if e != nil {
		return e
	}
	if !st.Mode().IsRegular() || st.Size() != a.Size {
		return errors.New("staged installer is not a regular file of expected size")
	}
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer f.Close()
	h := sha256.New()
	n, e := io.Copy(h, io.LimitReader(f, a.Size+1))
	if e != nil || n != a.Size || !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), a.SHA256) {
		return errors.New("staged installer integrity check failed")
	}
	return nil
}
func (m *Manager) VerifiedPath() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state.Artifact == nil || m.state.Staged == "" || m.envelope == nil {
		return "", errors.New("no staged installer")
	}
	if _, _, e := m.verify(*m.envelope); e != nil {
		return "", e
	}
	if e := verifyFile(m.state.Staged, *m.state.Artifact); e != nil {
		return "", e
	}
	return m.state.Staged, nil
}
