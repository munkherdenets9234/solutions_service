package config

import (
	"strings"
	"testing"
)

func TestRequestEmailFeature(t *testing.T) {
	cases := []struct {
		name, url, svc, key string
		want                bool
	}{
		{"all set", "http://tc", "svc", strings.Repeat("k", 32), true},
		{"no link", "", "", strings.Repeat("k", 32), false},
		{"no signing key", "http://tc", "svc", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := validConfig()
			c.TenantcoreURL, c.TenantcoreServiceKey, c.MailUnsubscribeKey = tc.url, tc.svc, tc.key
			if got := c.RequestEmailEnabled(); got != tc.want {
				t.Fatalf("RequestEmailEnabled() = %v, want %v", got, tc.want)
			}
			var f *Feature
			for _, x := range c.Features() {
				if x.Name == "request_email" {
					x := x
					f = &x
				}
			}
			if f == nil || f.Enabled != tc.want {
				t.Fatalf("request_email feature = %+v, want Enabled=%v", f, tc.want)
			}
			if !tc.want {
				for _, name := range []string{"TENANTCORE_URL", "MAIL_UNSUBSCRIBE_KEY"} {
					if !strings.Contains(f.Detail, name) {
						t.Fatalf("detail should name %s: %q", name, f.Detail)
					}
				}
			}
		})
	}
}
