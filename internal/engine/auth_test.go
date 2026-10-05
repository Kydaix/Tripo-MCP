package engine

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kydaix/Tripo-MCP/internal/auth"
	"github.com/Kydaix/Tripo-MCP/internal/fault"
)

func expiringSession(t *testing.T, e *Engine) auth.Session {
	t.Helper()
	s, err := auth.Load(e.Root)
	if err != nil {
		t.Fatal(err)
	}
	claims, _ := json.Marshal(map[string]any{"sub": "test-account", "exp": time.Now().Add(45 * time.Second).Unix()})
	s.Token = "e30." + base64.RawURLEncoding.EncodeToString(claims) + ".test"
	s.Cookie = "fixture-only-cookie"
	if err := auth.Save(e.Root, s); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestExistingClientRenewsBeforeNextRequest(t *testing.T) {
	var calls, refreshes atomic.Int32
	e := setup(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Cookie") != "" {
			t.Error("session cookie leaked to Studio operation")
		}
		io.WriteString(w, `{"code":0,"data":{}}`)
	})
	_, client, err := e.client(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	expiringSession(t, e)
	e.Renew = func(_ context.Context, s auth.Session) (auth.Session, error) {
		refreshes.Add(1)
		claims, _ := json.Marshal(map[string]any{"sub": "test-account", "exp": time.Now().Add(time.Hour).Unix()})
		s.Token = "e30." + base64.RawURLEncoding.EncodeToString(claims) + ".test"
		return s, nil
	}
	if _, err := client.Account(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || refreshes.Load() != 1 {
		t.Fatal("existing client did not refresh at request time")
	}
	if err := auth.Logout(e.Root); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Account(context.Background()); err == nil {
		t.Fatal("old client ignored logout")
	}
	if calls.Load() != 1 {
		t.Fatal("request sent after logout")
	}
}

func TestRenewalFailureDoesNotSubmitPaidOperation(t *testing.T) {
	var calls atomic.Int32
	e := setup(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); t.Error("unexpected Studio request") })
	expiringSession(t, e)
	e.Renew = func(context.Context, auth.Session) (auth.Session, error) {
		return auth.Session{}, fault.New("AUTH_EXPIRED", "fixture")
	}
	_, err := e.Generate(context.Background(), request())
	if fault.Public(err).Code != "AUTH_EXPIRED" || calls.Load() != 0 {
		t.Fatal("renewal failure dispatched generation")
	}
	jobs, _ := e.List()
	if len(jobs) != 0 {
		t.Fatal("renewal failure created a paid job")
	}
}

func TestExistingClientRejectsDifferentAccount(t *testing.T) {
	e := setup(t, func(w http.ResponseWriter, r *http.Request) { t.Error("request sent with another account") })
	_, client, err := e.client(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	claims, _ := json.Marshal(map[string]any{"sub": "another-account", "exp": time.Now().Add(time.Hour).Unix()})
	s, err := auth.FromHeader("Bearer e30."+base64.RawURLEncoding.EncodeToString(claims)+".test", "device", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := auth.Save(e.Root, s); err != nil {
		t.Fatal(err)
	}
	_, err = client.Account(context.Background())
	if fault.Public(err).Code != "ACCOUNT_CHANGED" || strings.Contains(err.Error(), s.Token) {
		t.Fatal("account guard failed")
	}
}
