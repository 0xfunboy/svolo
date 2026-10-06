package updates

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

type fixture struct {
	manager   *Manager
	secret    ed25519.PrivateKey
	manifest  Manifest
	envelope  Envelope
	body      []byte
	trustFile string
	server    *httptest.Server
}

func setup(t *testing.T) *fixture {
	t.Helper()
	pub, priv, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	f := &fixture{secret: priv, body: []byte("non-executable installer fixture: never run")}
	f.server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/release":
			json.NewEncoder(w).Encode(f.envelope)
		case "/artifact":
			w.Write(f.body)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.server.Close)
	u, _ := url.Parse(f.server.URL)
	root := t.TempDir()
	f.trustFile = filepath.Join(root, "publisher.json")
	trust := Trust{Feed: f.server.URL + "/release", Channel: "preview", Keys: map[string]string{"test-publisher": base64.StdEncoding.EncodeToString(pub)}, AllowedHosts: []string{u.Host}}
	raw, _ := json.Marshal(trust)
	if e = os.WriteFile(f.trustFile, raw, 0600); e != nil {
		t.Fatal(e)
	}
	f.manager, e = New(root, "0.3.0-dev", f.trustFile)
	if e != nil {
		t.Fatal(e)
	}
	f.manager.client.Transport = f.server.Client().Transport
	hash := sha256.Sum256(f.body)
	now := time.Now()
	f.manifest = Manifest{Product: "svolo", Channel: "preview", Version: "0.3.1-dev", Sequence: 10, Published: now.Add(-time.Hour), Expires: now.Add(24 * time.Hour), Notes: "Fixture release", Artifacts: []Artifact{{OS: runtime.GOOS, Arch: runtime.GOARCH, Kind: "zip", URL: f.server.URL + "/artifact", Size: int64(len(f.body)), SHA256: hex.EncodeToString(hash[:])}}}
	f.sign()
	return f
}
func (f *fixture) sign() {
	raw, _ := json.Marshal(f.manifest)
	f.envelope = Envelope{KeyID: "test-publisher", Payload: base64.StdEncoding.EncodeToString(raw), Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(f.secret, raw))}
}
func TestSignedUpdateStagesAndReverifies(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	st, e := f.manager.Check(ctx)
	if e != nil || !st.SignatureVerified || st.NativeSignatureVerified || st.Phase != "available" {
		t.Fatalf("check: %+v %v", st, e)
	}
	st, e = f.manager.Stage(ctx)
	if e != nil || st.Staged == "" || st.Phase != "staged" {
		t.Fatalf("stage: %+v %v", st, e)
	}
	p, e := f.manager.VerifiedPath()
	if e != nil || p != st.Staged {
		t.Fatalf("path: %s %v", p, e)
	}
	b, _ := os.ReadFile(p)
	if string(b) != string(f.body) {
		t.Fatal("wrong downloaded bytes")
	}
	os.WriteFile(p, []byte("tampered"), 0600)
	if _, e = f.manager.VerifiedPath(); e == nil {
		t.Fatal("staged tampering accepted")
	}
}
func TestPublisherFailures(t *testing.T) {
	cases := map[string]func(*fixture){
		"signature":    func(f *fixture) { f.envelope.Signature = base64.StdEncoding.EncodeToString(make([]byte, 64)) },
		"unknown-key":  func(f *fixture) { f.envelope.KeyID = "outsider" },
		"product":      func(f *fixture) { f.manifest.Product = "unrelated-product"; f.sign() },
		"channel":      func(f *fixture) { f.manifest.Channel = "stable"; f.sign() },
		"expired":      func(f *fixture) { f.manifest.Expires = time.Now().Add(-time.Second); f.sign() },
		"future":       func(f *fixture) { f.manifest.Published = time.Now().Add(time.Hour); f.sign() },
		"lifetime":     func(f *fixture) { f.manifest.Expires = time.Now().Add(40 * 24 * time.Hour); f.sign() },
		"insecure-url": func(f *fixture) { f.manifest.Artifacts[0].URL = "http://example.com/file"; f.sign() },
		"foreign-host": func(f *fixture) { f.manifest.Artifacts[0].URL = "https://outsider.invalid/file"; f.sign() },
		"huge":         func(f *fixture) { f.manifest.Artifacts[0].Size = MaxArtifact + 1; f.sign() },
		"duplicate": func(f *fixture) {
			f.manifest.Artifacts = append(f.manifest.Artifacts, f.manifest.Artifacts[0])
			f.sign()
		},
		"missing-platform": func(f *fixture) {
			if runtime.GOARCH == "amd64" {
				f.manifest.Artifacts[0].Arch = "arm64"
			} else {
				f.manifest.Artifacts[0].Arch = "amd64"
			}
			f.sign()
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			f := setup(t)
			change(f)
			if _, e := f.manager.Check(context.Background()); e == nil {
				t.Fatal("invalid release accepted")
			}
		})
	}
}
func TestRollbackLedgerSurvivesRestart(t *testing.T) {
	f := setup(t)
	if _, e := f.manager.Check(context.Background()); e != nil {
		t.Fatal(e)
	}
	m, e := New(f.manager.root, "0.3.0-dev", f.trustFile)
	if e != nil {
		t.Fatal(e)
	}
	m.client.Transport = f.manager.client.Transport
	f.manifest.Sequence--
	f.sign()
	if _, e = m.Check(context.Background()); e == nil {
		t.Fatal("rollback accepted")
	}
	f.manifest.Sequence++
	f.manifest.Notes = "same sequence, different content"
	f.sign()
	if _, e = m.Check(context.Background()); e == nil {
		t.Fatal("equivocation accepted")
	}
}
func TestArtifactFailures(t *testing.T) {
	for _, which := range []string{"different-hash", "truncated", "oversize", "expired-after-check", "symlink"} {
		t.Run(which, func(t *testing.T) {
			f := setup(t)
			if _, e := f.manager.Check(context.Background()); e != nil {
				t.Fatal(e)
			}
			switch which {
			case "different-hash":
				f.body[0] ^= 1
			case "truncated":
				f.body = f.body[:3]
			case "oversize":
				f.body = append(f.body, 1)
			case "expired-after-check":
				f.manager.now = func() time.Time { return time.Now().Add(3 * 24 * time.Hour) }
			case "symlink":
				if runtime.GOOS == "windows" {
					t.Skip("requires symlink privilege")
				}
				dir := filepath.Join(f.manager.root, "updates")
				os.MkdirAll(dir, 0700)
				p := filepath.Join(dir, "svolo-"+f.manifest.Version+"-"+runtime.GOOS+"-"+runtime.GOARCH+".zip")
				if e := os.Symlink(f.trustFile, p); e != nil {
					t.Fatal(e)
				}
			}
			if _, e := f.manager.Stage(context.Background()); e == nil {
				t.Fatal("invalid artifact staged")
			}
			if _, e := f.manager.VerifiedPath(); e == nil {
				t.Fatal("failed artifact exposed")
			}
		})
	}
}
func TestNoImplicitUpdateAuthority(t *testing.T) {
	m, e := New(t.TempDir(), "0.3.0-dev", "")
	if e != nil {
		t.Fatal(e)
	}
	if m.Status().Configured {
		t.Fatal("implicit trust")
	}
	if _, e = m.Check(context.Background()); e == nil {
		t.Fatal("disabled channel checked")
	}
	if _, e = m.Stage(context.Background()); e == nil {
		t.Fatal("disabled channel staged")
	}
}
func TestUpdateTrustValidation(t *testing.T) {
	f := setup(t)
	for _, which := range []string{"wildcard", "no-keys", "weak-key", "bad-channel", "userinfo", "fragment"} {
		t.Run(which, func(t *testing.T) {
			var trust Trust
			b, _ := os.ReadFile(f.trustFile)
			json.Unmarshal(b, &trust)
			switch which {
			case "wildcard":
				trust.AllowedHosts = []string{"*.example.com"}
			case "no-keys":
				trust.Keys = nil
			case "weak-key":
				trust.Keys["weak"] = "AA=="
			case "bad-channel":
				trust.Channel = "anything"
			case "userinfo":
				trust.Feed = strings.Replace(trust.Feed, "https://", "https://user:password@", 1)
			case "fragment":
				trust.Feed += "#x"
			}
			if e := trust.Validate(); e == nil {
				t.Fatal("invalid trust configuration accepted")
			}
		})
	}
}
func TestSemanticVersionOrdering(t *testing.T) {
	for _, pair := range [][2]string{{"0.3.0-dev", "0.3.0"}, {"0.3.0-beta.2", "0.3.0-beta.10"}, {"1.9.0", "1.10.0"}, {"2.0.0", "3.0.0"}, {"1.0.0-alpha", "1.0.0-alpha.1"}, {"1.0.0-1", "1.0.0-a"}} {
		c, e := Compare(pair[0], pair[1])
		if e != nil || c >= 0 {
			t.Fatalf("%v: %d %v", pair, c, e)
		}
	}
	if _, e := Compare("../../x", "0.3.0"); e == nil {
		t.Fatal("invalid semver accepted")
	}
}
