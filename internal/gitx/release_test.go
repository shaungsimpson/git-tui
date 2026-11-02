package gitx

import "testing"

func TestParseRelease(t *testing.T) {
	if l, err := ParseRelease(`{"tag_name":"v0.5.0","draft":false,"prerelease":false}`); err != nil || l.Version != "v0.5.0" {
		t.Fatalf("got %+v, %v", l, err)
	}
	for _, out := range []string{
		``, `not json`, `{}`, `{"message":"Not Found"}`,
		`{"tag_name":"v0.6.0","prerelease":true}`,
		`{"tag_name":"v0.6.0","draft":true}`,
		`{"tag_name":"v0.6.0-rc.1"}`,
		`{"tag_name":"v0.6.0\u001b[31m"}`,
		`{"tag_name":7}`,
	} {
		if l, err := ParseRelease(out); err == nil {
			t.Errorf("%s: accepted as %+v", out, l)
		}
	}
}

func TestDemoVersions(t *testing.T) {
	for _, tc := range []struct{ stamp, running, latest string }{
		{"v0.10.0", "v0.10.0", "v0.11.0"},                               // go install …@v0.10.0, or a clean checkout on the tag
		{"v0.10.1-0.20261102034117-5711216da85a", "v0.10.0", "v0.11.0"}, // a checkout after the tag
		{"v0.10.1-0.20261102034117-5711216da85a+dirty", "v0.10.0", "v0.11.0"},
		{"v0.11.0-rc.1", "v0.11.0", "v0.12.0"},
		{"(devel)", "v0.4.1", "v0.5.0"},
		{"", "v0.4.1", "v0.5.0"},
		{"v0.0.0-20261102034117-5711216da85a", "v0.4.1", "v0.5.0"}, // no tag yet
	} {
		if b, l := demoVersions(tc.stamp); b.Version != tc.running || l != tc.latest {
			t.Errorf("%q: got %s and %s, want %s and %s", tc.stamp, b.Version, l, tc.running, tc.latest)
		}
	}
}
