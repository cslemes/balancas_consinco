package pathguard

import (
	"path/filepath"
	"strings"
)

// IsSafeComponent reports whether name can be used as one local path component
// on both Unix and Windows hosts.
func IsSafeComponent(name string) bool {
	if name == "" || name == "." || name == ".." || !filepath.IsLocal(name) || strings.ContainsAny(name, `/\\<>:"|?*`) {
		return false
	}
	if strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") {
		return false
	}
	device := strings.ToUpper(strings.SplitN(name, ".", 2)[0])
	if device == "CON" || device == "PRN" || device == "AUX" || device == "NUL" {
		return false
	}
	if len(device) == 4 && (strings.HasPrefix(device, "COM") || strings.HasPrefix(device, "LPT")) && device[3] >= '1' && device[3] <= '9' {
		return false
	}
	return true
}
