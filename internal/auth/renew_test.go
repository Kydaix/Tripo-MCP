package auth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kydaix/Tripo-MCP/internal/fault"
	"github.com/Kydaix/Tripo-MCP/internal/local"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func renewable(t *testing.T) Session {
	t.Helper()
	s, err := parseHeader("Bearer "+token(t, time.Now().Add(-time.Minute)), "fixture-device", "ov")
	if err != nil {
		t.Fatal(err)
	}
	s.Cookie = "fixture-session-cookie"
	s.SessionExpires = time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)
	return s
}

func responseClient(t *testing.T, status int, body string, cookies ...string) *http.Client {
	t.Helper()
	return &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }, Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != sessionURL || r.Method != "GET" {
			t.Error("wrong renewal endpoint")
		}
		if r.Header.Get("Authorization") != "" || r.Header.Get("X-Tripo-Device-Id") != "" {
			t.Error("extra credential on renewal")
		}
		if got := r.Cookies(); len(got) != 1 || got[0].Name != cookieName || got[0].Value != "fixture-session-cookie" {
			t.Error("unexpected cookies")
		}
		h := make(http.Header)
		for _, c := range cookies {
			h.Add("Set-Cookie", c)
		}
		return &http.Response{StatusCode: status, Header: h, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
}

func TestRenewContractAndProtectedPersistence(t *testing.T) {
	s := renewable(t)
	jwt := token(t, time.Now().Add(10*time.Minute))
	body, _ := json.Marshal(map[string]any{"tokenized": jwt, "active": true, "expires_at": s.SessionExpires})
	next, err := renewSession(context.Background(), s, responseClient(t, 200, string(body), cookieName+"=rotated-fixture; Path=/; Secure; HttpOnly", "analytics=ignored"))
	if err != nil {
		t.Fatal(err)
	}
	if next.Token != jwt || next.Cookie != "rotated-fixture" || next.Account != s.Account || !next.SessionExpires.Equal(s.SessionExpires) {
		t.Fatal("renewal metadata lost")
	}
	root := t.TempDir()
	if err := Save(root, next); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(root, "session.dpapi"))
	if bytes.Contains(raw, []byte(jwt)) || bytes.Contains(raw, []byte(next.Cookie)) {
		t.Fatal("credentials in plaintext")
	}
	loaded, err := Load(root)
	if err != nil || loaded.Cookie != next.Cookie {
		t.Fatal("renewable session not persisted")
	}
	public, _ := json.Marshal(Inspect(root))
	if bytes.Contains(public, []byte(jwt)) || bytes.Contains(public, []byte(next.Cookie)) || bytes.Contains(public, []byte(s.Account)) {
		t.Fatal("private state in status")
	}
}

func TestRenewRejectsBadResponsesAndAccountSwitch(t *testing.T) {
	s := renewable(t)
	other, _ := json.Marshal(map[string]any{"sub": "different-fixture-account", "exp": time.Now().Add(time.Hour).Unix()})
	otherJWT := "e30." + base64.RawURLEncoding.EncodeToString(other) + ".test"
	for _, tc := range []struct {
		code       int
		body, want string
	}{
		{401, "private remote message", "AUTH_EXPIRED"}, {403, "<html>challenge</html>", "ACCESS_DENIED"},
		{429, "private remote message", "RATE_LIMITED"}, {503, "private remote message", "HTTP_ERROR"},
		{302, "private remote message", "HTTP_ERROR"}, {200, "not json", "PROTOCOL_CHANGED"},
		{200, `{"tokenized":"` + otherJWT + `"}`, "ACCOUNT_CHANGED"},
		{200, `{"active":false}`, "AUTH_EXPIRED"},
		{200, `{"tokenized":"` + token(t, time.Now().Add(time.Hour)) + `","active":false}`, "AUTH_EXPIRED"},
	} {
		_, err := renewSession(context.Background(), s, responseClient(t, tc.code, tc.body))
		if err == nil || fault.Public(err).Code != tc.want {
			t.Fatalf("HTTP %d: expected %s, got %v", tc.code, tc.want, err)
		}
		if strings.Contains(err.Error(), "private remote") || strings.Contains(err.Error(), otherJWT) {
			t.Fatal("remote secret leaked")
		}
	}
}

func TestConcurrentRefreshIsSharedAndSurvivesRestart(t *testing.T) {
	root := t.TempDir()
	s := renewable(t)
	if err := Save(root, s); err != nil {
		t.Fatal(err)
	}
	if status := Inspect(root); !status.Authenticated || !status.Renewable || !status.NeedsRefresh {
		t.Fatal("expired token hid valid renewable session")
	}
	var calls atomic.Int32
	renew := func(ctx context.Context, input Session) (Session, error) {
		calls.Add(1)
		time.Sleep(40 * time.Millisecond)
		input.Token = token(t, time.Now().Add(10*time.Minute))
		return input, nil
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if _, err := Resolve(context.Background(), root, renew); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if _, err := Resolve(context.Background(), root, renew); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || Inspect(root).NeedsRefresh {
		t.Fatal("duplicate refresh or stale token persisted")
	}
}

func TestResolveRevocationAndTransientFailure(t *testing.T) {
	for _, code := range []string{"AUTH_EXPIRED", "NETWORK_ERROR", "ACCESS_DENIED"} {
		t.Run(code, func(t *testing.T) {
			root := t.TempDir()
			if err := Save(root, renewable(t)); err != nil {
				t.Fatal(err)
			}
			_, err := Resolve(context.Background(), root, func(context.Context, Session) (Session, error) { return Session{}, fault.New(code, "fixture") })
			if fault.Public(err).Code != code {
				t.Fatal("wrong refresh error")
			}
			_, statErr := os.Stat(filepath.Join(root, "session.dpapi"))
			if (code == "AUTH_EXPIRED") != os.IsNotExist(statErr) {
				t.Fatal("revoked credentials kept or transient failure removed credentials")
			}
		})
	}
}

func TestResolveDoesNotResurrectLogoutAndHonorsCancellation(t *testing.T) {
	root := t.TempDir()
	Save(root, renewable(t))
	unlock, err := local.Lock(filepath.Join(root, "auth.lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	never := func(context.Context, Session) (Session, error) { t.Error("unexpected renewal"); return Session{}, nil }
	_, err = Resolve(ctx, root, never)
	if fault.Public(err).Code != "BUSY" {
		t.Fatal("refresh lock ignored cancellation")
	}
	if err := Logout(root); err != nil {
		t.Fatal(err)
	}
	_, err = Resolve(context.Background(), root, never)
	if fault.Public(err).Code != "AUTH_REQUIRED" {
		t.Fatal("logout resurrected")
	}
}

func TestCookieValidationAndLegacySession(t *testing.T) {
	for _, value := range []string{"", "value; other=secret", "value\r\nHeader: injection", "white space", "quote\"", "back\\slash", strings.Repeat("a", 8193)} {
		if _, err := sessionCookie(value); err == nil {
			t.Fatal("invalid cookie accepted")
		}
	}
	if value, err := sessionCookie(cookieName + "=abc%2F=="); err != nil || value != "abc%2F==" {
		t.Fatal("cookie encoding altered")
	}
	s, _ := FromHeader("Bearer "+token(t, time.Now().Add(time.Hour)), "fixture-device", "")
	root := t.TempDir()
	if err := Save(root, s); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(context.Background(), root, func(context.Context, Session) (Session, error) {
		t.Fatal("legacy session attempted renewal")
		return Session{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if Inspect(root).Renewable {
		t.Fatal("legacy session advertised as renewable")
	}
}
