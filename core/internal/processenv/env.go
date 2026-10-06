// Package processenv prevents provider/vault credentials from being inherited by
// child tools. This is credential minimization, NOT an OS sandbox.
package processenv

import (
	"os"
	"strings"
)

var allowed = map[string]bool{"PROCESSOR_ARCHITECTURE": true, "PATH": true, "HOME": true, "USER": true, "LOGNAME": true, "SHELL": true, "LANG": true, "LANGUAGE": true, "TZ": true, "TERM": true, "COLORTERM": true, "TMPDIR": true, "TMP": true, "TEMP": true, "SYSTEMROOT": true, "WINDIR": true, "COMSPEC": true, "PATHEXT": true, "USERPROFILE": true, "APPDATA": true, "LOCALAPPDATA": true, "PROGRAMFILES": true, "PROGRAMFILES(X86)": true, "COMMONPROGRAMFILES": true, "ALLUSERSPROFILE": true, "DISPLAY": true, "XAUTHORITY": true, "WAYLAND_DISPLAY": true, "XDG_RUNTIME_DIR": true, "XDG_SESSION_TYPE": true, "XDG_CURRENT_DESKTOP": true, "DBUS_SESSION_BUS_ADDRESS": true, "AT_SPI_BUS_ADDRESS": true, "GTK_MODULES": true, "GTK_A11Y": true, "NO_AT_BRIDGE": true, "XDG_DATA_DIRS": true, "XDG_CONFIG_HOME": true, "XDG_DATA_HOME": true, "SSL_CERT_FILE": true, "SSL_CERT_DIR": true, "SSH_AUTH_SOCK": true}

func Safe() []string { return Filter(os.Environ()) }
func Filter(env []string) []string {
	out := []string{}
	for _, v := range env {
		k, _, ok := strings.Cut(v, "=")
		k = strings.ToUpper(k)
		if ok && (allowed[k] || strings.HasPrefix(k, "LC_")) {
			out = append(out, v)
		}
	}
	return out
}

// SSH preserves user configuration variables (including ProxyCommand needs), but
// application-private master keys must never be eligible for SendEnv forwarding.
// Provider credentials should use vault references rather than ambient env.
func SSH() []string {
	out := []string{}
	for _, v := range os.Environ() {
		k, _, _ := strings.Cut(v, "=")
		k = strings.ToUpper(k)
		if strings.HasSuffix(k, "VAULT_KEY") || strings.HasSuffix(k, "_NATIVE_TOKEN") || strings.HasSuffix(k, "_CORE_TOKEN") {
			continue
		}
		out = append(out, v)
	}
	return out
}
