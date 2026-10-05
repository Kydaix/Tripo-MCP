package auth

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestPortalRejectsCrossOriginAndRebinding(t *testing.T) {
	p := &portal{root: t.TempDir(), capability: "test-capability", done: make(chan Status, 1), verify: func(context.Context, Session) error { t.Fatal("unexpected verification"); return nil }}
	srv := httptest.NewServer(p)
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	p.host = u.Host
	for _, tc := range []struct{ host, origin, key string }{{u.Host, "https://evil.test", p.capability}, {u.Host, srv.URL, "wrong"}, {"evil.test", srv.URL, p.capability}} {
		req, _ := http.NewRequest("POST", srv.URL+"/session", strings.NewReader(`{}`))
		req.Host = tc.host
		req.Header.Set("Origin", tc.origin)
		req.Header.Set("X-Tripo-MCP-Transfer", tc.key)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 403 {
			t.Fatalf("accepted forbidden origin/capability/host: %d", resp.StatusCode)
		}
	}
}

func TestPortalStoresOnlyVerifiedSessionAndStops(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	root := t.TempDir()
	jwt := token(t, time.Now().Add(time.Hour))
	verified := false
	var address string
	status, err := Login(ctx, root, func(link string) {
		u, _ := url.Parse(link)
		capability := u.Fragment
		u.Fragment = ""
		address = u.String()
		page, err := http.Get(address)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(page.Body)
		page.Body.Close()
		if !strings.Contains(string(body), "Connecter Tripo Studio") || strings.Contains(string(body), capability) {
			t.Fatal("bad or secret-bearing page")
		}
		input, _ := json.Marshal(map[string]string{"authorization": "Bearer " + jwt, "device_id": "test-device", "region": ""})
		req, _ := http.NewRequest("POST", address+"session", strings.NewReader(string(input)))
		req.Header.Set("Origin", strings.TrimSuffix(address, "/"))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Tripo-MCP-Transfer", capability)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 || strings.Contains(string(b), jwt) {
			t.Fatalf("transfer failed or leaked credentials: %d", resp.StatusCode)
		}
	}, func(_ context.Context, s Session) error {
		verified = true
		if s.Token != jwt {
			t.Fatal("wrong token")
		}
		return nil
	})
	if err != nil || !status.Authenticated || !verified {
		t.Fatalf("%+v %v", status, err)
	}
	if loaded, err := Load(root); err != nil || loaded.Token != jwt {
		t.Fatal("protected session missing")
	}
	if resp, err := http.Get(address); err == nil {
		resp.Body.Close()
		t.Fatal("temporary service still listening")
	}
}

func TestPortalVerificationFailureDoesNotSave(t *testing.T) {
	p := &portal{root: t.TempDir(), host: "127.0.0.1:12345", capability: "test", done: make(chan Status, 1), verify: func(context.Context, Session) error { return context.DeadlineExceeded }}
	b, _ := json.Marshal(map[string]string{"authorization": "Bearer " + token(t, time.Now().Add(time.Hour)), "device_id": "test-device"})
	req := httptest.NewRequest("POST", "http://"+p.host+"/session", strings.NewReader(string(b)))
	req.Header.Set("Origin", "http://"+p.host)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tripo-MCP-Transfer", p.capability)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)
	if w.Code != 400 || Inspect(p.root).Authenticated {
		t.Fatal("unverified session persisted")
	}
}

func TestPortalVerifiesRenewableCookieBeforeSaving(t *testing.T) {
	root := t.TempDir()
	s := renewable(t)
	renewed, verified := false, false
	p := &portal{root: root, host: "127.0.0.1:12345", capability: "fixture", done: make(chan Status, 1),
		renew: func(_ context.Context, input Session) (Session, error) {
			renewed = true
			if input.Cookie != s.Cookie || input.Account != s.Account {
				t.Fatal("cookie or identity lost")
			}
			input.Token = token(t, time.Now().Add(time.Hour))
			input.Expires = time.Now().Add(time.Hour)
			input.SessionExpires = s.SessionExpires
			return input, nil
		}, verify: func(_ context.Context, input Session) error {
			verified = true
			if !renewed {
				t.Error("account checked before renewal")
			}
			return nil
		}}
	b, _ := json.Marshal(map[string]string{"authorization": "Bearer " + s.Token, "device_id": s.DeviceID, "session_cookie": s.Cookie})
	r := httptest.NewRequest("POST", "http://"+p.host+"/session", strings.NewReader(string(b)))
	r.Header.Set("Origin", "http://"+p.host)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Tripo-MCP-Transfer", p.capability)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, r)
	if w.Code != 200 || !renewed || !verified || !Inspect(root).Renewable {
		t.Fatal("renewable transfer failed")
	}
	if strings.Contains(w.Body.String(), s.Cookie) || strings.Contains(w.Body.String(), s.Token) {
		t.Fatal("secret in portal response")
	}
}
