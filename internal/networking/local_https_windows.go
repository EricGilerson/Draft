//go:build windows

package networking

import (
	"strings"

	"Draft/internal/executil"
)

// certutil's Root store is the LocalMachine Trusted Root Certification
// Authorities store. It requires elevation, supplied by the existing UAC
// launcher used by the local DNS installer.
func installLocalHTTPSRoot(path, _ string) error {
	return runElevatedPowerShell("certutil.exe -f -addstore Root '" + strings.ReplaceAll(path, "'", "''") + "'")
}

func removeLocalHTTPSRoot(_ string, sha1 string) error {
	return runElevatedPowerShell("certutil.exe -delstore Root " + sha1)
}

func localHTTPSRootTrusted(_ string, sha1 string) (bool, error) {
	out, err := executil.Command("certutil.exe", "-store", "Root", sha1).CombinedOutput()
	if err != nil {
		return false, nil
	}
	return strings.Contains(strings.ToUpper(string(out)), strings.ToUpper(sha1)), nil
}
