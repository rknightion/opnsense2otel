package redact

import (
	"net/url"
	"testing"
)

func TestURL(t *testing.T) {
	credentialURL := url.URL{
		Scheme: "https", Host: "service.example", Path: "/path",
		User: url.UserPassword("user", "pass"), RawQuery: "token=sample&x=1",
	}
	cases := map[string]string{
		credentialURL.String():                              "https://%5BREDACTED%5D@service.example/path?token=%5BREDACTED%5D&x=1",
		"https://service.example/a%2Fb?z=1&x=hello%20world": "https://service.example/a%2Fb?z=1&x=hello%20world",
		"relative?token=sample":                             "relative?token=sample",
	}
	for input, want := range cases {
		if got := URL(input); got != want {
			t.Errorf("URL(%q) = %q, want %q", input, got, want)
		}
	}
}
