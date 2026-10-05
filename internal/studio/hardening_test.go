package studio

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Kydaix/Tripo-MCP/internal/auth"
	"github.com/Kydaix/Tripo-MCP/internal/fault"
)

func TestImageContentMustMatchExtensionBeforeNetwork(t *testing.T) {
	for _, tc := range []struct {
		name, content string
		valid         bool
	}{
		{"fake.png", "<html>not an image</html>", false},
		{"wrong.jpg", "\x89PNG\r\n\x1a\nfixture", false},
		{"image.png", "\x89PNG\r\n\x1a\nfixture", true},
		{"image.jpeg", "\xff\xd8\xfffixture", true},
		{"image.webp", "RIFF\x08\x00\x00\x00WEBPVP8 ", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), tc.name)
			if err := os.WriteFile(path, []byte(tc.content), 0600); err != nil {
				t.Fatal(err)
			}
			_, err := ValidateImage(path)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v error=%v", tc.valid, err)
			}
			if !tc.valid {
				c := New(auth.Session{})
				c.Credentials = func(context.Context) (auth.Session, error) {
					t.Fatal("invalid image reached network preparation")
					return auth.Session{}, nil
				}
				if _, err = c.Upload(context.Background(), path); err == nil {
					t.Fatal("invalid image uploaded")
				}
			}
		})
	}
}

func TestNullDataAndRateLimitsCannotLookSuccessful(t *testing.T) {
	for _, code := range []int{200, 429} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(code)
			_, _ = io.WriteString(w, `{"code":0,"data":null}`)
		}))
		c := New(auth.Session{})
		c.BaseURL = srv.URL
		_, err := c.Account(context.Background())
		want := "PROTOCOL_CHANGED"
		if code == 429 {
			want = "RATE_LIMITED"
		}
		if err == nil || fault.Public(err).Code != want {
			t.Fatalf("read: %v", err)
		}
		_, err = c.Generate(context.Background(), GenerateInput{Prompt: "fixture"}, nil)
		if !IsUncertain(err) {
			t.Fatalf("write should stay uncertain: %v", err)
		}
		srv.Close()
	}
}

func TestBlankTextureAndInvalidAnimationAreRejected(t *testing.T) {
	if _, err := (EditInput{Operation: "texture", Prompt: " \n\t"}).Normalize(); err == nil {
		t.Fatal("blank paid texture accepted")
	}
	for _, name := range []string{"", " \t", strings.Repeat("x", 257)} {
		if _, err := (ExportOptions{WithAnimation: true, Animations: []string{name}}).Normalize(); err == nil {
			t.Fatal("invalid animation accepted")
		}
	}
}
