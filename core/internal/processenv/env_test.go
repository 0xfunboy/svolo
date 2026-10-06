package processenv

import (
	"strings"
	"testing"
)

func TestSecretsNotInherited(t *testing.T) {
	v := strings.Join(Filter([]string{"PATH=/bin", "SVOLO_VAULT_KEY=secret", "OPENAI_API_KEY=key", "CUSTOM_CREDENTIAL=other", "LD_PRELOAD=evil", "NODE_OPTIONS=evil", "DISPLAY=:7", "LC_ALL=en_US.UTF-8"}), ";")
	if strings.Contains(v, "secret") || strings.Contains(v, "evil") || strings.Contains(v, "other") || strings.Contains(v, "=key") {
		t.Fatal(v)
	}
	if !strings.Contains(v, "PATH=") || !strings.Contains(v, "DISPLAY=") {
		t.Fatal(v)
	}
}

func TestSSHDoesNotExposePrivateMasterKeys(t *testing.T) {
	t.Setenv("SVOLO_VAULT_KEY", "private-master")
	t.Setenv("SVOLO_NATIVE_TOKEN", "private-native")
	t.Setenv("SVOLO_CORE_TOKEN", "private-bearer")
	t.Setenv("SVOLO_PROXY_CONFIG", "user-proxy-setting")
	joined := strings.Join(SSH(), "\n")
	if strings.Contains(joined, "private-master") || strings.Contains(joined, "private-native") || strings.Contains(joined, "private-bearer") {
		t.Fatal("private key inherited")
	}
	if !strings.Contains(joined, "SVOLO_PROXY_CONFIG=user-proxy-setting") {
		t.Fatal("user ProxyCommand environment discarded")
	}
}
