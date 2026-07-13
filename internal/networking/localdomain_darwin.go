//go:build darwin

package networking

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const resolverMarker = "# Draft managed local domain — do not edit"

// localDNSPort is the unprivileged loopback port for Draft's DNS responder.
// Port 53 requires root and is already owned by the system stack on macOS.
const localDNSPort = "53535"

func localDNSAddr() string { return "127.0.0.1:" + localDNSPort }

func installLocalDomainResolver() error {
	path := filepath.Join("/etc/resolver", LocalSuffix)
	// macOS resolver(5) documents "nameserver 127.0.0.1.53535" (IP with a
	// trailing .<port>), but mDNSResponder — the process that actually serves
	// /etc/resolver lookups on modern macOS — only reliably honors a separate
	// `port` keyword. The dotted form is treated as a bare nameserver address
	// (or ignored), so queries go to 127.0.0.1:53, time out for ~30–60s, and
	// verification fails with "did not resolve .draft to loopback".
	content := resolverMarker + "\n" +
		"# Routes only *." + LocalSuffix + " queries to Draft's loopback DNS listener.\n" +
		"nameserver 127.0.0.1\n" +
		"port " + localDNSPort + "\n"
	tmp, err := os.CreateTemp("", "draft-resolver-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if err := tmp.Close(); err != nil {
		return err
	}
	defer os.Remove(tmpPath)
	if err := os.WriteFile(tmpPath, []byte(content), 0o600); err != nil {
		return err
	}
	quoted := shellQuote(tmpPath)
	// HUP mDNSResponder so the new /etc/resolver entry is picked up before we
	// run the OS-level verification lookup. Best-effort: ignore killall errors.
	cmd := fmt.Sprintf(
		"/bin/mkdir -p /etc/resolver && /bin/cp %s %s && /bin/chmod 644 %s && /usr/bin/killall -HUP mDNSResponder 2>/dev/null || true",
		quoted, shellQuote(path), shellQuote(path),
	)
	return runAppleScriptAdmin(cmd)
}

func removeLocalDomainResolver() error {
	path := filepath.Join("/etc/resolver", LocalSuffix)
	// Never remove a resolver file which was not installed by Draft.
	cmd := fmt.Sprintf(
		"if [ -f %s ] && /usr/bin/grep -Fq %s %s; then /bin/rm %s; fi; /usr/bin/killall -HUP mDNSResponder 2>/dev/null || true",
		shellQuote(path), shellQuote(resolverMarker), shellQuote(path), shellQuote(path),
	)
	return runAppleScriptAdmin(cmd)
}

func localDomainResolverInstalled() (bool, error) {
	data, err := os.ReadFile(filepath.Join("/etc/resolver", LocalSuffix))
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	text := string(data)
	if !strings.Contains(text, resolverMarker) || !strings.Contains(text, "127.0.0.1") {
		return false, nil
	}
	// Accept the current `port N` form and the legacy dotted form so a prior
	// install still shows as Draft-owned and can be removed cleanly.
	return strings.Contains(text, "port "+localDNSPort) ||
		strings.Contains(text, "127.0.0.1."+localDNSPort), nil
}

func runAppleScriptAdmin(command string) error {
	// osascript presents macOS's standard administrator prompt. The command is
	// built solely from fixed paths and our own temporary file.
	script := `do shell script "` + strings.ReplaceAll(command, `"`, `\\"`) + `" with administrator privileges`
	out, err := exec.Command("/usr/bin/osascript", "-e", script).CombinedOutput()
	if err != nil {
		return fmt.Errorf("local .draft authorization: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func shellQuote(v string) string { return "'" + strings.ReplaceAll(v, "'", "'\\''") + "'" }
