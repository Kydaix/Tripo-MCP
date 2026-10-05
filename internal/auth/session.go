// Package auth handles Studio sessions without passwords or public API keys.
package auth

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Kydaix/Tripo-MCP/internal/fault"
	"github.com/Kydaix/Tripo-MCP/internal/local"
)

// Session is private transport state. Never return it from a tool or print it.
type Session struct {
	Token          string    `json:"token"`
	DeviceID       string    `json:"device_id"`
	Region         string    `json:"region,omitempty"`
	Account        string    `json:"account"`
	Expires        time.Time `json:"expires_at"`
	Cookie         string    `json:"session_cookie,omitempty"`
	SessionExpires time.Time `json:"session_expires_at,omitempty"`
}

type Status struct {
	Authenticated  bool       `json:"authenticated"`
	Expires        time.Time  `json:"expires_at,omitempty"`
	Renewable      bool       `json:"renewable"`
	NeedsRefresh   bool       `json:"needs_refresh"`
	SessionExpires *time.Time `json:"session_expires_at,omitempty"`
}

func FromHeader(header, device, region string) (Session, error) {
	s, err := parseHeader(header, device, region)
	if err == nil && !s.Expires.After(time.Now().Add(30*time.Second)) {
		return Session{}, fault.New("AUTH_EXPIRED", "Session expirée ; lancer tripo-mcp login.")
	}
	return s, err
}

func parseHeader(header, device, region string) (Session, error) {
	var s Session
	if !strings.HasPrefix(header, "Bearer ") || len(header) > 16384 || len(device) > 256 || len(region) > 32 || strings.ContainsAny(device+region, "\r\n") {
		return s, fault.New("AUTH_REQUIRED", "Session Studio invalide.")
	}
	token := strings.TrimPrefix(header, "Bearer ")
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return s, fault.New("AUTH_REQUIRED", "Format de session Studio inconnu.")
	}
	b, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return s, fault.New("AUTH_REQUIRED", "Session Studio illisible.")
	}
	var claims map[string]any
	if json.Unmarshal(b, &claims) != nil {
		return s, fault.New("AUTH_REQUIRED", "Session Studio illisible.")
	}
	exp, ok := claims["exp"].(float64)
	if !ok || exp <= 0 || exp > 253402300799 {
		return s, fault.New("AUTH_REQUIRED", "Expiration de session invalide.")
	}
	identity := ""
	for _, key := range []string{"sub", "user_id", "userId", "uid", "account_id"} {
		if value, ok := claims[key]; ok && value != nil {
			identity = fmt.Sprint(value)
			if identity != "" {
				break
			}
		}
	}
	if identity == "" {
		return s, fault.New("AUTH_REQUIRED", "Identité Studio absente de la session.")
	}
	hash := sha256.Sum256([]byte(identity))
	return Session{Token: token, DeviceID: device, Region: region, Account: hex.EncodeToString(hash[:]), Expires: time.Unix(int64(exp), 0).UTC()}, nil
}

func Save(root string, s Session) error {
	// Re-derive identity and expiry instead of trusting an imported record.
	verified, err := validate(s)
	if err != nil {
		return err
	}
	b, err := json.Marshal(verified)
	if err != nil {
		return err
	}
	defer clear(b)
	protected, err := local.Protect(b)
	if err != nil {
		return err
	}
	return local.Write(filepath.Join(root, "session.dpapi"), protected)
}

func Load(root string) (Session, error) {
	var s Session
	b, err := os.ReadFile(filepath.Join(root, "session.dpapi"))
	if err != nil {
		return s, fault.New("AUTH_REQUIRED", "Connexion requise : tripo-mcp login.")
	}
	b, err = local.Unprotect(b)
	if err != nil {
		return s, err
	}
	defer clear(b)
	if json.Unmarshal(b, &s) != nil {
		return s, fault.New("AUTH_REQUIRED", "Session illisible ; relancer tripo-mcp login.")
	}
	return validate(s)
}

func Inspect(root string) Status {
	s, err := Load(root)
	if err != nil {
		return Status{}
	}
	return s.Status()
}

// Status inspects local credentials only. It does not contact or modify Studio.
func (s Session) Status() Status {
	renewable := s.Cookie != "" && (s.SessionExpires.IsZero() || s.SessionExpires.After(time.Now()))
	v := Status{Authenticated: renewable || s.Expires.After(time.Now().Add(30*time.Second)), Expires: s.Expires, Renewable: renewable, NeedsRefresh: !s.Expires.After(time.Now().Add(time.Minute))}
	if !s.SessionExpires.IsZero() {
		v.SessionExpires = &s.SessionExpires
	}
	return v
}

func validate(s Session) (Session, error) {
	v, err := parseHeader("Bearer "+s.Token, s.DeviceID, s.Region)
	if err != nil {
		return Session{}, err
	}
	if s.Cookie != "" {
		v.Cookie, err = sessionCookie(s.Cookie)
		if err != nil {
			return Session{}, err
		}
		v.SessionExpires = s.SessionExpires
		if !v.SessionExpires.IsZero() && !v.SessionExpires.After(time.Now()) {
			return Session{}, fault.New("AUTH_EXPIRED", "Connexion Studio expirée ; refaire le transfert avec tripo-mcp login.")
		}
	} else if !v.Expires.After(time.Now().Add(30 * time.Second)) {
		return Session{}, fault.New("AUTH_EXPIRED", "Jeton temporaire expiré ; lancer tripo-mcp login et transférer le cookie de session pour activer le renouvellement.")
	}
	return v, nil
}

func Logout(root string) error {
	err := os.Remove(filepath.Join(root, "session.dpapi"))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
