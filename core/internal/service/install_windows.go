package service

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"unicode/utf16"
)

func psQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
func psEncoded(s string) string {
	u := utf16.Encode([]rune(s))
	b := make([]byte, len(u)*2)
	for i, v := range u {
		binary.LittleEndian.PutUint16(b[i*2:], v)
	}
	return base64.StdEncoding.EncodeToString(b)
}
func installUserUnit(ctx context.Context, o InstallOptions, args []string) (string, error) {
	name := "SvoloAgent-" + strconv.Itoa(o.Port)
	// Quote each argument for the Windows command-line parser, not for a shell.
	command := ""
	for _, a := range args {
		if command != "" {
			command += " "
		}
		command += `"` + strings.ReplaceAll(a, `"`, `\"`) + `"`
	}
	script := `$ErrorActionPreference='Stop';$name=` + psQuote(name) + `;$user=[System.Security.Principal.WindowsIdentity]::GetCurrent().Name;` +
		`$action=New-ScheduledTaskAction -Execute ` + psQuote(o.Executable) + ` -Argument ` + psQuote(command) + `;` +
		`$trigger=New-ScheduledTaskTrigger -AtLogOn -User $user;` +
		`$principal=New-ScheduledTaskPrincipal -UserId $user -LogonType Interactive -RunLevel Limited;` +
		`$settings=New-ScheduledTaskSettingsSet -ExecutionTimeLimit ([TimeSpan]::Zero) -RestartCount 3 -RestartInterval (New-TimeSpan -Minutes 1);` +
		`Register-ScheduledTask -TaskName $name -Action $action -Trigger $trigger -Principal $principal -Settings $settings -Force | Out-Null;Start-ScheduledTask -TaskName $name`
	b, err := exec.CommandContext(ctx, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", psEncoded(script)).CombinedOutput()
	if err != nil {
		return name, fmt.Errorf("scheduled task: %w: %.1000s", err, b)
	}
	return name, nil
}
