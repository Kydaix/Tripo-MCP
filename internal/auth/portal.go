package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Kydaix/Tripo-MCP/internal/fault"
	"github.com/Kydaix/Tripo-MCP/internal/local"
)

//go:embed web/index.html web/app.js web/session.mjs web/style.css
var portalFiles embed.FS

type portal struct {
	root, host, capability string
	verify                 func(context.Context, Session) error
	renew                  Renewer
	done                   chan Status
	mu                     sync.Mutex
	completed              bool
}

// Login starts a temporary loopback-only receiver. The human's browser is never controlled.
func Login(ctx context.Context, root string, ready func(string), verify func(context.Context, Session) error) (Status, error) {
	unlock, err := local.Lock(filepath.Join(root, "login.lock"))
	if err != nil {
		return Status{}, err
	}
	defer unlock()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return Status{}, fault.New("LOCAL_ERROR", "Impossible d'ouvrir le service local de connexion.")
	}
	defer listener.Close()
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	p := &portal{root: root, host: listener.Addr().String(), capability: rand.Text(), verify: verify, done: make(chan Status, 1)}
	server := &http.Server{Handler: p, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 100 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8192, BaseContext: func(net.Listener) context.Context { return ctx }}
	ended := make(chan error, 1)
	go func() { ended <- server.Serve(listener) }()
	defer func() {
		stop, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(stop)
	}()
	ready("http://" + p.host + "/#" + p.capability)
	select {
	case status := <-p.done:
		return status, nil
	case <-ctx.Done():
		return Status{}, fault.New("AUTH_REQUIRED", "Transfert non terminé ; relancer tripo-mcp login.")
	case <-ended:
		return Status{}, fault.New("LOCAL_ERROR", "Service local de connexion arrêté.")
	}
}

func (p *portal) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
	if r.Host != p.host {
		http.Error(w, "Hôte refusé", http.StatusForbidden)
		return
	}
	if r.URL.Path == "/session" {
		p.capture(w, r)
		return
	}
	if r.Method != "GET" {
		http.Error(w, "Méthode refusée", http.StatusMethodNotAllowed)
		return
	}
	files := map[string]struct{ name, mime string }{"/": {"index.html", "text/html; charset=utf-8"}, "/app.js": {"app.js", "text/javascript; charset=utf-8"}, "/session.mjs": {"session.mjs", "text/javascript; charset=utf-8"}, "/style.css": {"style.css", "text/css; charset=utf-8"}}
	f, ok := files[r.URL.Path]
	if !ok {
		http.NotFound(w, r)
		return
	}
	b, _ := portalFiles.ReadFile("web/" + f.name)
	w.Header().Set("Content-Type", f.mime)
	_, _ = w.Write(b)
}

func (p *portal) capture(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Méthode refusée", http.StatusMethodNotAllowed)
		return
	}
	if r.Header.Get("Origin") != "http://"+p.host || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Tripo-MCP-Transfer")), []byte(p.capability)) != 1 {
		http.Error(w, "Transfert refusé", http.StatusForbidden)
		return
	}
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		http.Error(w, "JSON requis", http.StatusUnsupportedMediaType)
		return
	}
	if !p.mu.TryLock() {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": fault.Public(fault.New("BUSY", "Un transfert est déjà en cours ; attendre sa fin avant de réessayer."))})
		return
	}
	defer p.mu.Unlock()
	if p.completed {
		http.Error(w, "Transfert déjà terminé", http.StatusConflict)
		return
	}
	var input struct {
		Authorization string `json:"authorization"`
		Device        string `json:"device_id"`
		Region        string `json:"region"`
		Cookie        string `json:"session_cookie"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32768))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil {
		http.Error(w, "Session illisible", http.StatusBadRequest)
		return
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		http.Error(w, "Entrée invalide", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	renew := p.renew
	if renew == nil {
		renew = Renew
	}
	s, err := Prepare(ctx, Session{Token: strings.TrimPrefix(input.Authorization, "Bearer "), DeviceID: input.Device, Region: input.Region, Cookie: input.Cookie}, renew)
	if err == nil {
		err = p.verify(ctx, s)
	}
	if ctx.Err() != nil {
		err = fault.New("NETWORK_ERROR", "La vérification n'a pas abouti à temps ; vérifier la connexion puis réessayer.")
	}
	if err == nil {
		var unlock func()
		unlock, err = local.Lock(filepath.Join(p.root, "auth.lock"))
		if err == nil {
			err = Save(p.root, s)
			unlock()
		}
	}
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": fault.Public(err)})
		return
	}
	status := s.Status()
	p.completed = true
	_ = json.NewEncoder(w).Encode(status)
	p.done <- status
}
