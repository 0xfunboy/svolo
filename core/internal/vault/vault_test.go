package vault

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEncryptedPersistenceAndAuthentication(t *testing.T) {
	root := t.TempDir()
	v := Open(root)
	key := strings.Repeat("a1", 32)
	if v.Put("key", "secret") == nil {
		t.Fatal("locked vault accepted write")
	}
	if err := v.Unlock(key); err != nil {
		t.Fatal(err)
	}
	if err := v.Put("provider", "PRIVATE_TEST_CREDENTIAL"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(root, "credentials.vault.json"))
	if strings.Contains(string(data), "PRIVATE_TEST") {
		t.Fatal("plaintext on disk")
	}
	v.Lock()
	if _, err := v.Get("provider"); err == nil {
		t.Fatal("locked read")
	}
	if err := v.Unlock(strings.Repeat("b2", 32)); err == nil {
		t.Fatal("wrong key accepted")
	}
	if err := v.Unlock(key); err != nil {
		t.Fatal(err)
	}
	if value, err := v.Get("provider"); err != nil || value != "PRIVATE_TEST_CREDENTIAL" {
		t.Fatal(value, err)
	}
	first := string(data)
	if err := v.Put("provider", "PRIVATE_TEST_CREDENTIAL"); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(filepath.Join(root, "credentials.vault.json"))
	if first == string(data) {
		t.Fatal("nonce reused")
	}
	if err := v.Delete("provider"); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Get("provider"); err == nil {
		t.Fatal("not deleted")
	}
}
func TestTamperAndInvalidNames(t *testing.T) {
	root := t.TempDir()
	v := Open(root)
	key := strings.Repeat("12", 32)
	v.Unlock(key)
	for _, name := range []string{"../a", "a/b", "", ".."} {
		if v.Put(name, "x") == nil {
			t.Fatal(name)
		}
	}
	v.Put("x", "y")
	v.Lock()
	os.WriteFile(filepath.Join(root, "credentials.vault.json"), []byte(`{"version":1,"nonce":"AA==","ciphertext":"AA=="}`), 0600)
	if v.Unlock(key) == nil {
		t.Fatal("tamper accepted")
	}
}
