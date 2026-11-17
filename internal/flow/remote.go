package flow

import (
	"net/url"
	"strings"
)

// ParseRemoteURL returns the owner and repository name that a git remote URL
// points at, for https, ssh:// and scp-like (git@host:owner/repo) URLs.
func ParseRemoteURL(remoteURL string) (owner string, name string, ok bool) {
	path := ""
	if parsed, err := url.Parse(remoteURL); err == nil && parsed.Scheme != "" && parsed.Host != "" {
		path = parsed.Path
	} else if _, after, found := strings.Cut(remoteURL, ":"); found && !strings.Contains(remoteURL, "://") {
		path = after
	} else {
		return "", "", false
	}

	parts := strings.Split(strings.Trim(strings.TrimSuffix(path, ".git"), "/"), "/")
	if len(parts) < 2 || parts[len(parts)-2] == "" || parts[len(parts)-1] == "" {
		return "", "", false
	}
	return parts[len(parts)-2], parts[len(parts)-1], true
}

// FindRemote returns the name of the remote pointing at owner/name, or "" if
// there is none.
func FindRemote(remoteURLs map[string]string, owner string, name string) string {
	match := ""
	for remote, remoteURL := range remoteURLs {
		o, n, ok := ParseRemoteURL(remoteURL)
		if !ok || !strings.EqualFold(o, owner) || !strings.EqualFold(n, name) {
			continue
		}
		// Prefer the conventional names, for a deterministic answer when
		// several remotes point at the same repository.
		if match == "" || remote == "upstream" || (remote == "origin" && match != "upstream") {
			match = remote
		}
	}
	return match
}
