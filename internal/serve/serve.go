// Package serve exposes the decrypted, in-memory files over loopback HTTP.
// Nothing is written to disk. A random URL "path-key" segment acts as a
// capability so other local processes that probe the port can't reach the assets.
package serve

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"
)

// Options configures a Server.
type Options struct {
	Addr       string        // bind address, default 127.0.0.1
	Port       int           // 0 = random
	PathKey    bool          // include a random path-key segment in the URL
	EntryPoint string        // directory document, default index.html
	Fallback   bool          // serve EntryPoint for unmatched non-file routes (SPA)
	Idle       time.Duration // auto-shutdown after this much inactivity; 0 = never
}

// Server serves an in-memory file set.
type Server struct {
	files   map[string][]byte
	opt     Options
	pathKey string
	ln      net.Listener
	lastReq time.Time
	mu      sync.Mutex
}

// New builds a Server and binds its listener (so URL is known before Run).
func New(files map[string][]byte, opt Options) (*Server, error) {
	if opt.Addr == "" {
		opt.Addr = "127.0.0.1"
	}
	if opt.EntryPoint == "" {
		opt.EntryPoint = "index.html"
	}
	s := &Server{files: files, opt: opt, lastReq: time.Now()}
	if opt.PathKey {
		var b [8]byte
		if _, err := rand.Read(b[:]); err != nil {
			return nil, err
		}
		s.pathKey = hex.EncodeToString(b[:])
	}
	ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", opt.Addr, opt.Port))
	if err != nil {
		return nil, err
	}
	s.ln = ln
	return s, nil
}

// URL is the address to open in a browser.
func (s *Server) URL() string {
	u := "http://" + s.ln.Addr().String() + "/"
	if s.opt.PathKey {
		u += s.pathKey + "/"
	}
	return u
}

// FileCount reports how many files are being served.
func (s *Server) FileCount() int { return len(s.files) }

// Run serves until ctx is cancelled or the idle timeout fires.
func (s *Server) Run(ctx context.Context) error {
	srv := &http.Server{Handler: s.handler()}

	idleCtx, cancelIdle := context.WithCancel(ctx)
	defer cancelIdle()
	if s.opt.Idle > 0 {
		go s.watchIdle(idleCtx, cancelIdle)
	}

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(s.ln) }()

	select {
	case <-idleCtx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
		return idleCtx.Err()
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func (s *Server) watchIdle(ctx context.Context, cancel context.CancelFunc) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.mu.Lock()
			idleFor := time.Since(s.lastReq)
			s.mu.Unlock()
			if idleFor >= s.opt.Idle {
				cancel()
				return
			}
		}
	}
}

func (s *Server) touch() {
	s.mu.Lock()
	s.lastReq = time.Now()
	s.mu.Unlock()
}

func (s *Server) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.touch()
		upath := strings.TrimPrefix(r.URL.Path, "/")

		if s.opt.PathKey {
			prefix := s.pathKey + "/"
			if upath != s.pathKey && !strings.HasPrefix(upath, prefix) {
				http.NotFound(w, r)
				return
			}
			upath = strings.TrimPrefix(strings.TrimPrefix(upath, s.pathKey), "/")
		}

		upath = path.Clean("/" + upath)[1:] // normalize, strip leading slash

		// Directory root -> entry point.
		if upath == "" {
			upath = s.opt.EntryPoint
		}

		if data, ok := s.files[upath]; ok {
			s.write(w, upath, data)
			return
		}
		// A path ending in "/" asks for that dir's entry point.
		if strings.HasSuffix(r.URL.Path, "/") {
			if data, ok := s.files[path.Join(upath, s.opt.EntryPoint)]; ok {
				s.write(w, s.opt.EntryPoint, data)
				return
			}
		}
		// SPA fallback: unmatched non-file routes serve the entry point.
		if s.opt.Fallback && !looksLikeFile(upath) {
			if data, ok := s.files[s.opt.EntryPoint]; ok {
				s.write(w, s.opt.EntryPoint, data)
				return
			}
		}
		http.NotFound(w, r)
	})
}

func (s *Server) write(w http.ResponseWriter, name string, data []byte) {
	ct := mime.TypeByExtension(path.Ext(name))
	if ct == "" {
		ct = http.DetectContentType(data)
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(data)
}

func looksLikeFile(p string) bool {
	return strings.Contains(path.Base(p), ".")
}
