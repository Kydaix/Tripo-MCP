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
	Token    string    `json:"token"`
	DeviceID string    `json:"device_id"`
	Region   string    `json:"region,omitempty"`
	Account  string    `json:"account"`
	Expires  time.Time `json:"expires_at"`
}

type Status struct {
	Authenticated bool      `json:"authenticated"`
	Expires       time.Time `json:"expires_at,omitempty"`
}

func FromHeader(header, device, region string) (Session, error) {
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
	if !ok || exp <= float64(time.Now().Unix()+30) {
		return s, fault.New("AUTH_EXPIRED", "Session expirée ; lancer tripo-mcp login.")
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
	verified, err := FromHeader("Bearer "+s.Token, s.DeviceID, s.Region)
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
	return FromHeader("Bearer "+s.Token, s.DeviceID, s.Region)
}

func Inspect(root string) Status {
	s, err := Load(root)
	if err != nil {
		return Status{}
	}
	return Status{Authenticated: true, Expires: s.Expires}
}

func Logout(root string) error {
	err := os.Remove(filepath.Join(root, "session.dpapi"))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
