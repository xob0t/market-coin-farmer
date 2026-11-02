package main

import "testing"

func TestAppVersionRegex(t *testing.T) {
	cases := []struct {
		name, page, want string
	}{
		// Current Market markup (Oct 2026) has no "-desktop." segment.
		{"current", `"-platform":"desktop","-version":"2026.10.17.0.t4814424588","-uid":"1"`, "2026.10.17.0.t4814424588"},
		{"legacy desktop suffix", `"-version":"2026.06.01.0-desktop.t123456"`, "2026.06.01.0-desktop.t123456"},
		{"js reference is ignored", `"-version":o.version,"-region":o.region`, ""},
	}
	for _, c := range cases {
		if got := extractStringValue([]byte(c.page), appVersionRegex); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
