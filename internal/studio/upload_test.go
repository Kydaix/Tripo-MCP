package studio

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Kydaix/Tripo-MCP/internal/auth"
	"github.com/Kydaix/Tripo-MCP/internal/fault"
)

func TestImageAuditVerdicts(t *testing.T) {
	for _, tc := range []struct {
		name, response, code string
	}{
		{"accepted", `{"code":0,"data":{"result":"pass"}}`, ""},
		{"rejected", `{"code":0,"data":{"result":"reject"}}`, "IMAGE_REJECTED"},
		{"sensitive", `{"code":0,"data":{"result":"sensitive"}}`, "IMAGE_REVIEW_REQUIRED"},
		{"nsfw", `{"code":0,"data":{"result":"nsfw","age_confirm_status":"unconfirmed"}}`, "IMAGE_REVIEW_REQUIRED"},
		{"invented normal value", `{"code":0,"data":{"result":"normal"}}`, "PROTOCOL_CHANGED"},
		{"missing verdict", `{"code":0,"data":{}}`, "PROTOCOL_CHANGED"},
		{"unknown verdict", `{"code":0,"data":{"result":"new-verdict"}}`, "PROTOCOL_CHANGED"},
		{"invalid type", `{"code":0,"data":{"result":true}}`, "PROTOCOL_CHANGED"},
		{"null data", `{"code":0,"data":null}`, "PROTOCOL_CHANGED"},
		{"server rejection", `{"code":4001,"data":{"result":"pass"}}`, "STUDIO_ERROR"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "POST" || r.URL.Path != "/v2/studio/audit/image" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				var body struct {
					Image Image `json:"image"`
				}
				if json.NewDecoder(r.Body).Decode(&body) != nil || body.Image.Bucket != "fixture-bucket" || body.Image.Key != "fixture.png" || body.Image.Audit != "" {
					t.Error("audit must send the uploaded resource without a fabricated verdict")
				}
				io.WriteString(w, tc.response)
			}))
			defer srv.Close()
			c := New(auth.Session{Token: "fixture"})
			c.BaseURL = srv.URL
			img := &Image{Bucket: "fixture-bucket", Key: "fixture.png", Source: "upload"}
			err := c.auditImage(context.Background(), img)
			if calls != 1 {
				t.Fatalf("audit called %d times", calls)
			}
			if tc.code != "" {
				if err == nil || fault.Public(err).Code != tc.code || img.Audit != "" {
					t.Fatalf("expected %s without an accepted verdict, got %v, %q", tc.code, err, img.Audit)
				}
				return
			}
			if err != nil || img.Audit != "pass" {
				t.Fatalf("valid image rejected: %v", err)
			}
			b, err := json.Marshal(img)
			if err != nil || !strings.Contains(string(b), `"image_audit_result":"pass"`) {
				t.Fatalf("accepted verdict missing from image payload: %s", b)
			}
		})
	}
}
