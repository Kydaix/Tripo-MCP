package auth

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/Kydaix/Tripo-MCP/internal/fault"
	"github.com/Kydaix/Tripo-MCP/internal/local"
)

const cookieName = "ory_kratos_session"
const sessionURL = "https://api.tripo3d.ai/v2/studio/studio/whoami?tokenizeAs=default_jwt"

type Renewer func(context.Context, Session) (Session, error)

// Only the Studio session cookie is retained, never a browser's complete cookie jar.
func sessionCookie(value string) (string, error) {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, cookieName+"=")
	if len(value) == 0 || len(value) > 8192 {
		return "", fault.New("INVALID_ARGUMENT", "Cookie de session Studio invalide.")
	}
	for _, c := range value {
		if c < 0x21 || c > 0x7e || strings.ContainsRune("\";,\\", c) {
			return "", fault.New("INVALID_ARGUMENT", "Copier uniquement la valeur du cookie ory_kratos_session.")
		}
	}
	return value, nil
}

// Prepare ignores imported identity/expiry metadata and verifies a renewable session immediately.
func Prepare(ctx context.Context, input Session, renew Renewer) (Session, error) {
	s, err := parseHeader("Bearer "+input.Token, input.DeviceID, input.Region)
	if err != nil {
		return Session{}, err
	}
	if s.DeviceID == "" {
		return Session{}, fault.New("INVALID_ARGUMENT", "En-tête x-tripo-device-id manquant.")
	}
	if input.Cookie == "" {
		return FromHeader("Bearer "+input.Token, input.DeviceID, input.Region)
	}
	s.Cookie, err = sessionCookie(input.Cookie)
	if err != nil {
		return Session{}, err
	}
	return renew(ctx, s)
}

func Renew(ctx context.Context, s Session) (Session, error) {
	client := &http.Client{Timeout: 25 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return renewSession(ctx, s, client)
}

func renewSession(ctx context.Context, s Session, client *http.Client) (Session, error) {
	cookie, err := sessionCookie(s.Cookie)
	if err != nil {
		return Session{}, err
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", sessionURL, nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: cookie})
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Origin", "https://studio.tripo3d.ai")
	req.Header.Set("Referer", "https://studio.tripo3d.ai/")
	req.Header.Set("User-Agent", "Tripo-MCP")
	resp, err := client.Do(req)
	if err != nil {
		return Session{}, fault.New("NETWORK_ERROR", "Renouvellement Studio indisponible ; réessayer plus tard.")
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 {
		return Session{}, fault.New("AUTH_EXPIRED", "Connexion Studio expirée ou révoquée ; refaire le transfert avec tripo-mcp login.")
	}
	if resp.StatusCode == 403 {
		return Session{}, fault.New("ACCESS_DENIED", "Renouvellement refusé ; ouvrir Studio dans le navigateur et terminer toute vérification humaine.")
	}
	if resp.StatusCode == 429 {
		return Session{}, fault.New("RATE_LIMITED", "Trop de renouvellements Studio ; réessayer plus tard.")
	}
	if resp.StatusCode != 200 {
		return Session{}, fault.New("HTTP_ERROR", "Le service de connexion Studio est indisponible ; réessayer plus tard.")
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil || len(b) > 1<<20 {
		return Session{}, fault.New("PROTOCOL_CHANGED", "Réponse de connexion Studio illisible.")
	}
	defer clear(b)
	var result struct {
		Token   string    `json:"tokenized"`
		Active  *bool     `json:"active"`
		Expires time.Time `json:"expires_at"`
	}
	if json.Unmarshal(b, &result) != nil {
		return Session{}, fault.New("PROTOCOL_CHANGED", "Format de renouvellement Studio inconnu.")
	}
	if (result.Active != nil && !*result.Active) || (!result.Expires.IsZero() && !result.Expires.After(time.Now())) {
		return Session{}, fault.New("AUTH_EXPIRED", "Connexion Studio expirée ; relancer tripo-mcp login.")
	}
	if result.Token == "" {
		return Session{}, fault.New("PROTOCOL_CHANGED", "Jeton absent de la réponse de renouvellement Studio.")
	}
	next, err := FromHeader("Bearer "+result.Token, s.DeviceID, s.Region)
	if err != nil {
		return Session{}, fault.New("PROTOCOL_CHANGED", "Studio n'a pas fourni de jeton valide après renouvellement.")
	}
	if next.Account != s.Account {
		return Session{}, fault.New("ACCOUNT_CHANGED", "Le cookie et le jeton appartiennent à des comptes différents ; refaire le transfert depuis le même compte Studio.")
	}
	next.Cookie, next.SessionExpires = cookie, result.Expires
	if next.SessionExpires.IsZero() {
		next.SessionExpires = s.SessionExpires
	}
	for _, c := range resp.Cookies() {
		if c.Name != cookieName {
			continue
		}
		if c.MaxAge < 0 || (!c.Expires.IsZero() && !c.Expires.After(time.Now())) {
			return Session{}, fault.New("AUTH_EXPIRED", "Studio a révoqué la connexion ; relancer tripo-mcp login.")
		}
		next.Cookie, err = sessionCookie(c.Value)
		if err != nil {
			return Session{}, fault.New("PROTOCOL_CHANGED", "Cookie de renouvellement Studio invalide.")
		}
	}
	return next, nil
}

// Resolve renews before a request, never by resubmitting a possibly paid request.
// The OS lock and re-read coalesce refreshes across CLI and MCP processes.
func Resolve(ctx context.Context, root string, renew Renewer) (Session, error) {
	s, err := Load(root)
	if err != nil || s.Cookie == "" || s.Expires.After(time.Now().Add(time.Minute)) {
		return s, err
	}
	ctx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	var unlock func()
	for {
		unlock, err = local.Lock(filepath.Join(root, "auth.lock"))
		if err == nil {
			break
		}
		if fault.Public(err).Code != "BUSY" {
			return Session{}, err
		}
		select {
		case <-ctx.Done():
			return Session{}, fault.New("BUSY", "Renouvellement de connexion en cours ; réessayer après sa fin.")
		case <-time.After(50 * time.Millisecond):
		}
	}
	defer unlock()
	s, err = Load(root)
	if err != nil || s.Cookie == "" || s.Expires.After(time.Now().Add(time.Minute)) {
		return s, err
	}
	next, err := renew(ctx, s)
	if err != nil {
		if fault.Public(err).Code == "AUTH_EXPIRED" {
			if removeErr := Logout(root); removeErr != nil {
				return Session{}, removeErr
			}
		}
		return Session{}, err
	}
	if next.Account != s.Account {
		return Session{}, fault.New("ACCOUNT_CHANGED", "Le compte Studio a changé pendant le renouvellement.")
	}
	if err := Save(root, next); err != nil {
		return Session{}, err
	}
	return next, nil
}
