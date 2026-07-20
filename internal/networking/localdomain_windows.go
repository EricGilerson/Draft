//go:build windows

package networking

import (
	"fmt"
	"strings"

	"Draft/internal/executil"
)

func localDNSAddr() string { return "127.0.0.1:53" }

func installLocalDomainResolver() error {
	return runElevatedPowerShell(`$r=Get-DnsClientNrptRule -ErrorAction SilentlyContinue | Where-Object {$_.DisplayName -eq 'Draft Local Domains'}; if (!$r) { Add-DnsClientNrptRule -Namespace '.draft' -NameServers '127.0.0.1' -DisplayName 'Draft Local Domains' -Comment 'Draft managed local domain' | Out-Null }`)
}

func removeLocalDomainResolver() error {
	return runElevatedPowerShell(`Get-DnsClientNrptRule -ErrorAction SilentlyContinue | Where-Object {$_.DisplayName -eq 'Draft Local Domains' -and $_.Comment -eq 'Draft managed local domain'} | Remove-DnsClientNrptRule -Force`)
}

func localDomainResolverInstalled() (bool, error) {
	out, err := executil.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", `(Get-DnsClientNrptRule -ErrorAction SilentlyContinue | Where-Object {$_.DisplayName -eq 'Draft Local Domains' -and $_.Comment -eq 'Draft managed local domain'} | Measure-Object).Count`).CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("inspect Draft DNS rule: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)) != "0", nil
}

func runElevatedPowerShell(script string) error {
	// Start-Process -Verb RunAs is the standard per-action UAC prompt. Draft
	// never runs its normal daemon elevated.
	encoded := strings.ReplaceAll(script, `'`, `''`)
	launcher := "Start-Process powershell -Verb RunAs -Wait -ArgumentList '-NoProfile -NonInteractive -Command \"" + encoded + "\"'"
	out, err := executil.Command("powershell", "-NoProfile", "-Command", launcher).CombinedOutput()
	if err != nil {
		return fmt.Errorf("local .draft authorization: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
