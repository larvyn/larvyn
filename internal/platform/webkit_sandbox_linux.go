package platform

import (
	"os"
	"strings"
)

// WebKitGTK 6.0 always runs its web process inside a bubblewrap sandbox, which
// needs unprivileged user namespaces. Ubuntu 24.04+ restricts those through
// AppArmor for unconfined programs, so bwrap fails ("setting up uid map:
// Permission denied") and the window crashes on start. When that restriction
// applies to us, fall back to WebKit without its sandbox: Larvyn only loads its
// own embedded frontend. A user who installed an AppArmor profile granting
// userns runs confined and keeps the sandbox.
func init() {
	const key = "WEBKIT_DISABLE_SANDBOX_THIS_IS_DANGEROUS"
	if _, set := os.LookupEnv(key); set {
		return
	}
	if usernsRestricted() {
		os.Setenv(key, "1")
	}
}

func usernsRestricted() bool {
	b, err := os.ReadFile("/proc/sys/kernel/apparmor_restrict_unprivileged_userns")
	if err != nil || strings.TrimSpace(string(b)) != "1" {
		return false
	}
	label, err := os.ReadFile("/proc/self/attr/apparmor/current")
	if err != nil {
		label, err = os.ReadFile("/proc/self/attr/current")
	}
	return err != nil || strings.TrimSpace(string(label)) == "unconfined"
}
