//go:build darwin

package networking

import (
	"fmt"
	"os/exec"
	"strings"
)

// Recent macOS releases explicitly reject a background process attempting to
// change trust settings (errAuthorizationInteractionNotAllowed). Open the CA
// in Keychain Access instead: that is the user-interactive, supported flow.
// The CA lives in the System keychain. This matches the prior add-trusted-cert
// attempt, which can add the certificate successfully before the trust-setting
// step is denied; do not send users to Login and create a duplicate.
func installLocalHTTPSRoot(path, sha1 string) error {
	if !darwinSystemCAInstalled(sha1) {
		cmd := fmt.Sprintf("/usr/bin/security add-certificates -k /Library/Keychains/System.keychain %s", shellQuote(path))
		if err := runAppleScriptAdmin(cmd); err != nil {
			// A prior add-trusted-cert can add the certificate first and fail only
			// when macOS rejects its non-interactive trust-settings change. The
			// certificate is then exactly where we need it; continue to the
			// interactive Keychain Access step rather than treating it as fatal.
			if !strings.Contains(err.Error(), "already in /Library/Keychains/System.keychain") {
				return fmt.Errorf("add Draft local CA to the System keychain: %w", err)
			}
		}
	}
	if err := exec.Command("/usr/bin/open", "-a", "Keychain Access").Start(); err != nil {
		return fmt.Errorf("open Draft local CA in Keychain Access: %w", err)
	}
	return fmt.Errorf("macOS requires you to trust Draft's local CA interactively: in Keychain Access select System, open ‘Draft Local HTTPS CA’, set Trust to Always Trust, authenticate, then enable Local HTTPS again")
}

func removeLocalHTTPSRoot(_ string, sha1 string) error {
	cmd := fmt.Sprintf("/usr/bin/security delete-certificate -Z %s /Library/Keychains/System.keychain 2>/dev/null || true", shellQuote(sha1))
	return runAppleScriptAdmin(cmd)
}

func localHTTPSRootTrusted(path, _ string) (bool, error) {
	out, err := exec.Command("/usr/bin/security", "verify-cert", "-c", path, "-k", "/Library/Keychains/System.keychain").CombinedOutput()
	if err != nil {
		return false, nil
	}
	return strings.TrimSpace(string(out)) == "" || strings.Contains(string(out), "...certificate verification successful"), nil
}

func darwinSystemCAInstalled(sha1 string) bool {
	// find-certificate takes its keychain as a positional argument, unlike
	// verify-cert. Passing -k made this probe fail and caused a duplicate add.
	out, err := exec.Command("/usr/bin/security", "find-certificate", "-a", "-Z", "/Library/Keychains/System.keychain").CombinedOutput()
	return err == nil && strings.Contains(strings.ToUpper(string(out)), strings.ToUpper(sha1))
}
