package resolve

import "strings"

// NormalizeLocator reduces any Git remote to host/owner/name: lowercased, with
// the scheme, credentials, port and .git suffix removed. It returns "" for an
// input that cannot name a repository across machines.
//
// A locator is synced discovery evidence, so a remote that only means something
// on this machine — an absolute or relative filesystem path, or a file:// URL —
// is deliberately not one. Those directories are Workspace bindings, which
// never leave the machine.
func NormalizeLocator(raw string) string {
	remote := strings.TrimSpace(raw)
	if remote == "" {
		return ""
	}

	if scheme, rest, ok := strings.Cut(remote, "://"); ok {
		if strings.EqualFold(scheme, "file") {
			return ""
		}
		remote = rest
	} else if strings.HasPrefix(remote, "/") || strings.HasPrefix(remote, ".") {
		return ""
	} else if at := strings.Index(remote, "@"); strings.Contains(remote, ":") {
		// scp-like form: [user@]host:path
		host, path, _ := strings.Cut(remote[at+1:], ":")
		remote = host + "/" + path
	} else {
		return ""
	}
	if at := strings.Index(remote, "@"); at >= 0 {
		remote = remote[at+1:]
	}

	remote = strings.TrimSuffix(strings.Trim(remote, "/"), ".git")
	remote = strings.ToLower(strings.Trim(remote, "/"))

	host, path, ok := strings.Cut(remote, "/")
	if !ok {
		return ""
	}
	if hostname, port, ok := strings.Cut(host, ":"); ok && isPort(port) {
		host = hostname
	}
	remote = host + "/" + path

	// host/owner/name is the shortest form that names a repository.
	if strings.Count(remote, "/") < 2 || strings.ContainsAny(remote, " \t") {
		return ""
	}
	return remote
}

func isPort(value string) bool {
	if value == "" {
		return false
	}
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}
