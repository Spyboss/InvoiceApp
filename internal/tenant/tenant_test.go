package tenant

import "testing"

func TestNormaliseHost(t *testing.T) {
	cases := map[string]string{
		"Example.COM":         "example.com",
		"example.com:443":     "example.com",
		"example.com.":        "example.com",
		"  example.com  ":     "example.com",
		"[::1]:8080":          "::1",
		"localhost:3000":      "localhost",
		"dealer.example.com.": "dealer.example.com",
		"":                    "",
	}
	for in, want := range cases {
		if got := NormaliseHost(in); got != want {
			t.Errorf("NormaliseHost(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLocalHosts(t *testing.T) {
	for _, host := range []string{"localhost", "127.0.0.1", "::1", "[::1]"} {
		if !isLocalHost(host) {
			t.Errorf("%q not treated as local", host)
		}
	}
	for _, host := range []string{"example.com", "localhost.example.com", "10.0.0.1"} {
		if isLocalHost(host) {
			t.Errorf("%q treated as local", host)
		}
	}
}
