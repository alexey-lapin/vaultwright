package serve

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func startTest(t *testing.T, files map[string][]byte, opt Options) (*Server, func()) {
	t.Helper()
	s, err := New(files, opt)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go s.Run(ctx)
	// wait until listener answers
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := http.Get(s.URL()); err == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	return s, cancel
}

func get(t *testing.T, url string) (int, string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestEntryPointAndPathKey(t *testing.T) {
	files := map[string][]byte{
		"index.html": []byte("<h1>home</h1>"),
		"app.js":     []byte("console.log(1)"),
	}
	s, stop := startTest(t, files, Options{PathKey: true})
	defer stop()

	if code, body := get(t, s.URL()); code != 200 || !strings.Contains(body, "home") {
		t.Fatalf("entry point: code=%d body=%q", code, body)
	}
	if code, _ := get(t, s.URL()+"app.js"); code != 200 {
		t.Fatalf("app.js: code=%d", code)
	}
	// Bare port without the path-key should 404.
	base := "http://" + s.ln.Addr().String() + "/"
	if code, _ := get(t, base+"index.html"); code != 404 {
		t.Fatalf("bare path should 404, got %d", code)
	}
}

func TestFallback(t *testing.T) {
	files := map[string][]byte{"index.html": []byte("SPA")}

	s, stop := startTest(t, files, Options{Fallback: true})
	defer stop()
	if code, body := get(t, s.URL()+"some/client/route"); code != 200 || body != "SPA" {
		t.Fatalf("fallback route: code=%d body=%q", code, body)
	}
	// A path that looks like a missing asset still 404s.
	if code, _ := get(t, s.URL()+"missing.css"); code != 404 {
		t.Fatalf("missing file should 404, got %d", code)
	}
}

func TestNoFallback404(t *testing.T) {
	files := map[string][]byte{"index.html": []byte("home")}
	s, stop := startTest(t, files, Options{})
	defer stop()
	if code, _ := get(t, s.URL()+"client/route"); code != 404 {
		t.Fatalf("without fallback unknown route should 404, got %d", code)
	}
}
