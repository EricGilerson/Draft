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

func localDNSAddr() string { return "127.0.0.1:53535" }

func installLocalDomainResolver() error {
	path := filepath.Join("/etc/resolver", LocalSuffix)
	content := resolverMarker + "\n# Routes only *.draft queries to Draft's loopback DNS listener.\nnameserver 127.0.0.1.53535\n"
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
	cmd := fmt.Sprintf("/bin/mkdir -p /etc/resolver && /bin/cp %s %s && /bin/chmod 644 %s", quoted, shellQuote(path), shellQuote(path))
	return runAppleScriptAdmin(cmd)
}

func removeLocalDomainResolver() error {
	path := filepath.Join("/etc/resolver", LocalSuffix)
	// Never remove a resolver file which was not installed by Draft.
	cmd := fmt.Sprintf("if [ -f %s ] && /usr/bin/grep -Fq %s %s; then /bin/rm %s; fi", shellQuote(path), shellQuote(resolverMarker), shellQuote(path), shellQuote(path))
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
	return strings.Contains(string(data), resolverMarker) && strings.Contains(string(data), "127.0.0.1.53535"), nil
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
