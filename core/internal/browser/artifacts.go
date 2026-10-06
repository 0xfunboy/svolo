package browser

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"svolo.local/core/internal/store"
	"time"
)

type Artifact struct {
	ID      string    `json:"id"`
	Session string    `json:"session"`
	Name    string    `json:"name"`
	Path    string    `json:"path"`
	Size    int64     `json:"size"`
	SHA256  string    `json:"sha256"`
	MIME    string    `json:"mimeType"`
	Created time.Time `json:"created"`
	Width   int       `json:"width,omitempty"`
	Height  int       `json:"height,omitempty"`
}

func SaveArtifact(root, sid, name string, b []byte) (Artifact, error) {
	if !store.ValidID(sid) || len(b) == 0 || len(b) > 64<<20 {
		return Artifact{}, errors.New("invalid or oversized artifact")
	}
	name = filepath.Base(name)
	if name == "." || strings.ContainsAny(name, "\x00\\:") {
		return Artifact{}, errors.New("invalid artifact name")
	}
	id := store.ID()
	dir := filepath.Join(root, "artifacts", sid, id)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return Artifact{}, err
	}
	sum := sha256.Sum256(b)
	a := Artifact{ID: id, Session: sid, Name: name, Path: filepath.Join(dir, name), Size: int64(len(b)), SHA256: hex.EncodeToString(sum[:]), MIME: http.DetectContentType(b), Created: time.Now().UTC()}
	if strings.HasPrefix(a.MIME, "image/") {
		cfg, _, err := image.DecodeConfig(bytes.NewReader(b))
		if err != nil {
			return Artifact{}, fmt.Errorf("invalid image artifact: %w", err)
		}
		a.Width = cfg.Width
		a.Height = cfg.Height
	}
	if err := store.Atomic(a.Path, b, 0600); err != nil {
		return Artifact{}, err
	}
	meta, _ := json.MarshalIndent(a, "", "  ")
	if err := store.Atomic(filepath.Join(dir, "metadata.json"), meta, 0600); err != nil {
		return Artifact{}, err
	}
	return a, nil
}
func ListArtifacts(root, sid string) ([]Artifact, error) {
	if !store.ValidID(sid) {
		return nil, errors.New("invalid session")
	}
	out := []Artifact{}
	entries, err := os.ReadDir(filepath.Join(root, "artifacts", sid))
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() || !store.ValidID(entry.Name()) {
			continue
		}
		var a Artifact
		b, err := os.ReadFile(filepath.Join(root, "artifacts", sid, entry.Name(), "metadata.json"))
		if err == nil && json.Unmarshal(b, &a) == nil {
			out = append(out, a)
		}
	}
	return out, nil
}
func VerifyArtifact(root, sid, id string) (Artifact, error) {
	if !store.ValidID(sid) || !store.ValidID(id) {
		return Artifact{}, errors.New("invalid artifact id")
	}
	dir := filepath.Join(root, "artifacts", sid, id)
	b, err := os.ReadFile(filepath.Join(dir, "metadata.json"))
	if err != nil {
		return Artifact{}, err
	}
	var a Artifact
	if err = json.Unmarshal(b, &a); err != nil {
		return a, err
	}
	if a.ID != id || a.Session != sid || filepath.Base(a.Name) != a.Name || strings.ContainsAny(a.Name, "\\:\x00") {
		return a, errors.New("artifact identity mismatch")
	}
	path := filepath.Join(dir, a.Name)
	st, err := os.Lstat(path)
	if err != nil || !st.Mode().IsRegular() {
		return a, errors.New("artifact missing or not a regular file")
	}
	if st.Size() != a.Size || a.Size == 0 || a.Size > 64<<20 {
		return a, errors.New("artifact size mismatch")
	}
	b, err = os.ReadFile(path)
	if err != nil {
		return a, err
	}
	sum := sha256.Sum256(b)
	if hex.EncodeToString(sum[:]) != a.SHA256 {
		return a, errors.New("artifact integrity mismatch")
	}
	a.Path = path
	return a, nil
}
