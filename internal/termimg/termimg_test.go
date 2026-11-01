package termimg

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestLinks(t *testing.T) {
	line := `See ![before](https://github.com/user-attachments/assets/aaa) and <img width="300" alt="after" src="https://github.com/user-attachments/assets/bbb" />`
	got := Links(line)
	if len(got) != 2 || got[0] != (Link{"before", "https://github.com/user-attachments/assets/aaa"}) ||
		got[1] != (Link{"after", "https://github.com/user-attachments/assets/bbb"}) {
		t.Fatalf("%+v", got)
	}
	if len(Links("no pictures [here](https://x)")) != 0 {
		t.Fatal("a plain link is not a picture")
	}
}

func TestAllowed(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://github.com/user-attachments/assets/426fe6f8":       true,
		"https://private-user-images.githubusercontent.com/1/x.png": true,
		"http://github.com/user-attachments/assets/426fe6f8":        false, // not https
		"https://github.com/acme/backend":                           false, // not an attachment
		"https://evil.example/x.png":                                false,
		"https://user@github.com/user-attachments/assets/x":         false,
		"https://169.254.169.254/latest/meta-data":                  false,
	} {
		if Allowed(raw) != want {
			t.Errorf("Allowed(%q) != %v", raw, want)
		}
	}
	if _, err := Download(context.Background(), "https://evil.example/x.png", "secret"); err == nil {
		t.Fatal("Download must refuse other hosts before connecting")
	}
}

// The token goes to GitHub, and is not forwarded when GitHub redirects to its storage host.
func TestTokenStaysWithGitHub(t *testing.T) {
	var storageAuth string
	storage := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		storageAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte("png"))
	}))
	defer storage.Close()
	var githubAuth string
	github := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		githubAuth = r.Header.Get("Authorization")
		// A different host name for the same local server, like github.com redirecting to S3.
		http.Redirect(w, r, strings.Replace(storage.URL, "127.0.0.1", "localhost", 1), http.StatusFound)
	}))
	defer github.Close()

	realClient, realHost := client.Transport, githubHost
	client.Transport = github.Client().Transport
	githubHost = "127.0.0.1"
	t.Cleanup(func() { client.Transport, githubHost = realClient, realHost })
	github.Client().Transport.(*http.Transport).TLSClientConfig.InsecureSkipVerify = true // localhost is not in the test certificate

	data, err := download(context.Background(), github.URL+"/user-attachments/assets/x", "secret")
	if err != nil || string(data) != "png" {
		t.Fatalf("download: %q %v", data, err)
	}
	if githubAuth != "Bearer secret" {
		t.Fatalf("GitHub should get the token, got %q", githubAuth)
	}
	if storageAuth != "" {
		t.Fatalf("the token leaked to the storage host: %q", storageAuth)
	}
}

func TestEncodeAndLines(t *testing.T) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 400, 200))); err != nil {
		t.Fatal(err)
	}
	img, err := Decode(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	p, err := EncodeImage(img, 0x123456)
	if err != nil {
		t.Fatal(err)
	}
	if p.Width != 400 || p.Height != 200 || p.Rows(20) != 5 { // 20 cols × (200/400) ÷ 2 for tall cells
		t.Fatalf("size %dx%d rows %d", p.Width, p.Height, p.Rows(20))
	}
	// Transmit is the protocol's default action, so the stored-only image carries no a= key and no placement.
	if strings.Contains(p.Transmit, "a=") || strings.Contains(p.Transmit, "U=1") || !strings.Contains(p.Transmit, "i=1193046") {
		t.Fatalf("want the image stored once, without a placement: %.80q", p.Transmit)
	}
	place := p.Place(20)
	for _, want := range []string{"a=p", "U=1", "i=1193046", "p=1", "c=20", "r=5"} {
		if !strings.Contains(place, want) {
			t.Fatalf("placement %q missing %s", place, want)
		}
	}
	if len(place) > 100 {
		t.Fatalf("a resize should send a few bytes, not the image: %d", len(place))
	}
	lines := p.Lines(20)
	if len(lines) != 5 || lipgloss.Width(lines[0]) != 20 {
		t.Fatalf("placeholder rows: %d, width %d", len(lines), lipgloss.Width(lines[0]))
	}
	if _, err := Decode([]byte("<svg/>")); err == nil {
		t.Fatal("SVG is not supported and must say so")
	}
}

func TestSupported(t *testing.T) {
	env := func(kv map[string]string) func(string) string { return func(k string) string { return kv[k] } }
	if !Supported(env(map[string]string{"TERM_PROGRAM": "ghostty"})) {
		t.Fatal("Ghostty draws Kitty placeholders")
	}
	if Supported(env(map[string]string{"TERM_PROGRAM": "ghostty", "TMUX": "/tmp/x"})) {
		t.Fatal("inside tmux images are off")
	}
	if Supported(env(map[string]string{"TERM_PROGRAM": "Apple_Terminal"})) {
		t.Fatal("Terminal.app has no Kitty graphics")
	}
	if Supported(env(map[string]string{"TERM_PROGRAM": "WezTerm"})) {
		t.Fatal("WezTerm stays off until placeholders are tested there")
	}
	if !Supported(env(map[string]string{"TERM": "xterm-kitty"})) {
		t.Fatal("kitty draws placeholders")
	}
}

func TestAvatarRedirectsStayOnHost(t *testing.T) {
	req := func(raw string) *http.Request { r, _ := http.NewRequest(http.MethodGet, raw, nil); return r }
	avatar := []*http.Request{req("https://avatars.githubusercontent.com/u/1?s=128")}
	if checkRedirect(req("https://evil.example/x.png"), avatar) == nil {
		t.Fatal("an avatar must not redirect to another host")
	}
	if err := checkRedirect(req("https://avatars.githubusercontent.com/u/1?s=128&v=4"), avatar); err != nil {
		t.Fatalf("a same-host avatar redirect is fine: %v", err)
	}
	attachment := []*http.Request{req("https://github.com/user-attachments/assets/a")}
	if err := checkRedirect(req("https://github-production-user-asset-6210df.s3.amazonaws.com/a"), attachment); err != nil {
		t.Fatalf("attachments still follow GitHub to its storage: %v", err)
	}
}
