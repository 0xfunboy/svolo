package ssh

import (
	"reflect"
	"strings"
	"testing"
)

func TestHostInputValidation(t *testing.T) {
	h := Host{ID: "dev", Alias: "dev-box", RemotePort: 47831, TokenEnv: "SVOLO_DEV_TOKEN"}
	if e := Validate(h); e != nil {
		t.Fatal(e)
	}
	for _, alias := range []string{"-oProxyCommand=evil", "x; rm", "x$(id)", "x\ny"} {
		h.Alias = alias
		if Validate(h) == nil {
			t.Fatal("accepted", alias)
		}
	}
}
func TestTunnelSecurity(t *testing.T) {
	h := Host{ID: "s", Alias: "my-host", RemotePort: 47831, TokenEnv: "TOKEN"}
	a := TunnelArgs(h, 20000)
	joined := strings.Join(a, " ")
	for _, s := range []string{"StrictHostKeyChecking=yes", "BatchMode=yes", "ExitOnForwardFailure=yes", "ForwardAgent=no", "127.0.0.1:20000:127.0.0.1:47831"} {
		if !strings.Contains(joined, s) {
			t.Fatal(a)
		}
	}
	if !reflect.DeepEqual(a[len(a)-2:], []string{"--", "my-host"}) {
		t.Fatal(a)
	}
}
