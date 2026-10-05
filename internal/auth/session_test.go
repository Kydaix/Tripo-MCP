package auth

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func token(t *testing.T, exp time.Time) string {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"sub": "fixture-user", "exp": exp.Unix()})
	return "e30." + base64.RawURLEncoding.EncodeToString(b) + ".test"
}

func TestProtectedSessionAndLogout(t *testing.T) {
	root := t.TempDir()
	jwt := token(t, time.Now().Add(time.Hour))
	s, err := FromHeader("Bearer "+jwt, "device", "")
	if err != nil {
		t.Fatal(err)
	}
	if err = Save(root, s); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(root, "session.dpapi"))
	if bytes.Contains(b, []byte(jwt)) {
		t.Fatal("plaintext token on disk")
	}
	loaded, err := Load(root)
	if err != nil || loaded.Token != jwt {
		t.Fatal("session round trip failed")
	}
	os.WriteFile(filepath.Join(root, "keep.glb"), []byte("user file"), 0600)
	if err = Logout(root); err != nil {
		t.Fatal(err)
	}
	if Inspect(root).Authenticated {
		t.Fatal("still authenticated")
	}
	if _, err = os.Stat(filepath.Join(root, "keep.glb")); err != nil {
		t.Fatal("unrelated file removed")
	}
}

func TestInvalidSession(t *testing.T) {
	for _, h := range []string{"", "Bearer invalid", "Bearer " + token(t, time.Now().Add(-time.Hour))} {
		if _, err := FromHeader(h, "device", ""); err == nil {
			t.Fatal("accepted invalid session")
		}
	}
	if _, err := FromHeader("Bearer "+token(t, time.Now().Add(time.Hour)), "bad\r\nheader", ""); err == nil {
		t.Fatal("accepted header injection")
	}
}
