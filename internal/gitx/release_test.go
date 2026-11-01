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
