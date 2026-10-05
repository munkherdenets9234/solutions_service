// Package domainnorm holds the one definition of what a tenant's registered
// domain looks like once stored or compared: a bare lowercase host. It is a
// package of its own so the service that stores domains and the middleware that
// compares them against the request origin cannot drift apart.
package domainnorm

import "strings"

// Normalize lowercases and trims a domain and strips scheme, path and port.
func Normalize(domain string) string {
	domain = strings.ToLower(strings.TrimSpace(domain))
	domain = strings.TrimPrefix(domain, "https://")
	domain = strings.TrimPrefix(domain, "http://")
	domain = strings.TrimSuffix(domain, "/")
	if i := strings.IndexAny(domain, "/:"); i != -1 {
		domain = domain[:i]
	}
	return domain
}
