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
		{"nothing mail-related set", "http://tc", "svc", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := validConfig()
			c.TenantcoreURL, c.TenantcoreServiceKey, c.MailUnsubscribeKey = tc.url, tc.svc, tc.key
			if tc.key == "" {
				c.PublicBaseURL, c.AdminBaseURL = "", ""
			}
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
				for _, name := range []string{"MAIL_UNSUBSCRIBE_KEY", "PUBLIC_BASE_URL", "ADMIN_BASE_URL"} {
					if !strings.Contains(f.Detail, name) {
						t.Fatalf("detail should name %s: %q", name, f.Detail)
					}
				}
			}
		})
	}
}
