package domainnorm

import "testing"

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"https://Site.Example/": "site.example",
		"http://site.example":   "site.example",
		"site.example:8443":     "site.example",
		"site.example/path":     "site.example",
		"  Site.Example  ":      "site.example",
		"site.example":          "site.example",
		"":                      "",
		"   ":                   "",
	}
	for in, want := range cases {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}
