// Package vault stores AES-256-GCM encrypted credentials. The 256-bit wrapping
// key must be provided by the OS credential store or an external secret manager;
// it is NEVER written next to the encrypted file. This is not encryption of
// browser profiles or conversation history.
package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"svolo.local/core/internal/store"
)

var ErrLocked = errors.New("credential vault is locked; provide an external 256-bit wrapping key")

const aad = "svolo-vault-v1"

type entry struct {
	Value   string    `json:"value"`
	Updated time.Time `json:"updated"`
}
type envelope struct {
	Version    int    `json:"version"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}
type Info struct {
	Name    string    `json:"name"`
	Updated time.Time `json:"updated"`
}
type Vault struct {
	mu     sync.Mutex
	path   string
	key    []byte
	values map[string]entry
}

func Open(root string) *Vault { return &Vault{path: filepath.Join(root, "credentials.vault.json")} }
func ValidName(s string) bool { return store.ValidID(s) && !strings.Contains(s, "..") }
func (v *Vault) Unlock(keyHex string) error {
	key, err := hex.DecodeString(keyHex)
	if err != nil || len(key) != 32 {
		return errors.New("vault key must contain exactly 64 hexadecimal characters")
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	defer func() {
		for i := range key {
			key[i] = 0
		}
	}()
	if info, e := os.Lstat(v.path); e == nil && info.Mode()&os.ModeSymlink != 0 {
		return errors.New("vault file cannot be a symlink")
	}
	data, err := os.ReadFile(v.path)
	values := map[string]entry{}
	if err == nil {
		if len(data) > 8<<20 {
			return errors.New("vault size limit exceeded")
		}
		var env envelope
		if json.Unmarshal(data, &env) != nil || env.Version != 1 {
			return errors.New("invalid vault envelope")
		}
		block, _ := aes.NewCipher(key)
		gcm, _ := cipher.NewGCM(block)
		nonce, e := base64.StdEncoding.DecodeString(env.Nonce)
		if e != nil || len(nonce) != gcm.NonceSize() {
			return errors.New("invalid vault nonce")
		}
		encrypted, e := base64.StdEncoding.DecodeString(env.Ciphertext)
		if e != nil {
			return errors.New("invalid vault ciphertext")
		}
		plain, e := gcm.Open(nil, nonce, encrypted, []byte(aad))
		if e != nil {
			return errors.New("vault authentication failed; incorrect key or modified file")
		}
		e = json.Unmarshal(plain, &values)
		for i := range plain {
			plain[i] = 0
		}
		if e != nil {
			return errors.New("invalid vault payload")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	v.clearLocked()
	v.key = append([]byte(nil), key...)
	v.values = values
	return nil
}
func (v *Vault) clearLocked() {
	for i := range v.key {
		v.key[i] = 0
	}
	v.key = nil
	v.values = nil
}
func (v *Vault) Lock()          { v.mu.Lock(); defer v.mu.Unlock(); v.clearLocked() }
func (v *Vault) Unlocked() bool { v.mu.Lock(); defer v.mu.Unlock(); return len(v.key) == 32 }
func (v *Vault) Get(name string) (string, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.key == nil {
		return "", ErrLocked
	}
	value, ok := v.values[name]
	if !ok {
		return "", errors.New("credential reference not found")
	}
	return value.Value, nil
}
func (v *Vault) List() ([]Info, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.key == nil {
		return nil, ErrLocked
	}
	result := []Info{}
	for k, e := range v.values {
		result = append(result, Info{k, e.Updated})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}
func (v *Vault) Put(name, value string) error {
	if !ValidName(name) || len(value) == 0 || len(value) > 65536 {
		return errors.New("invalid credential name or size")
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.key == nil {
		return ErrLocked
	}
	if len(v.values) >= 512 {
		if _, ok := v.values[name]; !ok {
			return errors.New("credential count limit exceeded")
		}
	}
	old, exists := v.values[name]
	v.values[name] = entry{value, time.Now().UTC()}
	if err := v.save(); err != nil {
		if exists {
			v.values[name] = old
		} else {
			delete(v.values, name)
		}
		return err
	}
	return nil
}
func (v *Vault) Delete(name string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.key == nil {
		return ErrLocked
	}
	old, ok := v.values[name]
	if !ok {
		return nil
	}
	delete(v.values, name)
	if err := v.save(); err != nil {
		v.values[name] = old
		return err
	}
	return nil
}
func (v *Vault) save() error {
	plain, err := json.Marshal(v.values)
	if err != nil {
		return err
	}
	defer func() {
		for i := range plain {
			plain[i] = 0
		}
	}()
	block, err := aes.NewCipher(v.key)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return err
	}
	encrypted := gcm.Seal(nil, nonce, plain, []byte(aad))
	data, err := json.Marshal(envelope{1, base64.StdEncoding.EncodeToString(nonce), base64.StdEncoding.EncodeToString(encrypted)})
	if err != nil {
		return err
	}
	return store.Atomic(v.path, data, 0600)
}
