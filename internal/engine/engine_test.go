package engine

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kydaix/Tripo-MCP/internal/auth"
	"github.com/Kydaix/Tripo-MCP/internal/fault"
	"github.com/Kydaix/Tripo-MCP/internal/studio"
)

func setup(t *testing.T, handler http.HandlerFunc) *Engine {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	e := New(t.TempDir())
	claims, _ := json.Marshal(map[string]any{"sub": "test-account", "exp": time.Now().Add(time.Hour).Unix()})
	s, err := auth.FromHeader("Bearer e30."+base64.RawURLEncoding.EncodeToString(claims)+".test", "test-device", "")
	if err != nil {
		t.Fatal(err)
	}
	if err = auth.Save(e.Root, s); err != nil {
		t.Fatal(err)
	}
	e.Client = func(s auth.Session) *studio.Client { c := studio.New(s); c.BaseURL = srv.URL; return c }
	return e
}

func request() GenerateRequest {
	return GenerateRequest{GenerateInput: studio.GenerateInput{Prompt: "a small owl"}, RequestID: "request-1", Confirm: true}
}

func TestGenerationReceiptSurvivesRestartAndCannotDoubleCharge(t *testing.T) {
	var calls atomic.Int32
	e := setup(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/v2/studio/operation/text_to_model" {
			t.Errorf("unexpected endpoint %s", r.URL.Path)
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["visibility"] != "private" || body["face_limit"] != float64(20000) {
			t.Errorf("unexpected defaults: %v", body)
		}
		io.WriteString(w, `{"code":0,"data":{"project_id":"project-1","operator_id":"operator-1"}}`)
	})
	in := request()
	v, err := e.Generate(context.Background(), in)
	if err != nil || v.State != "submitted" {
		t.Fatalf("%+v %v", v, err)
	}
	restarted := New(e.Root)
	restarted.Client = e.Client
	v, err = restarted.Generate(context.Background(), in)
	if err != nil || v.OperatorID != "operator-1" || calls.Load() != 1 {
		t.Fatalf("resubmitted: %+v %v calls=%d", v, err, calls.Load())
	}
	in.Prompt = "another owl"
	_, err = restarted.Generate(context.Background(), in)
	if fault.Public(err).Code != "REQUEST_CONFLICT" || calls.Load() != 1 {
		t.Fatal("conflicting request submitted")
	}
	journal, err := os.ReadFile(filepath.Join(e.Root, "jobs", "request-1.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(journal), "Bearer") || strings.Contains(string(journal), "e30.") {
		t.Fatal("credential in journal")
	}
}

func TestUncertainGenerationIsNeverRetried(t *testing.T) {
	for _, response := range []string{"invalid", `{"code":0,"data":{}}`, `{"code":123,"data":{}}`} {
		t.Run(response, func(t *testing.T) {
			var calls atomic.Int32
			e := setup(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(500)
				io.WriteString(w, response)
			})
			v, err := e.Generate(context.Background(), request())
			if fault.Public(err).Code != "OUTCOME_UNKNOWN" || v.State != "outcome_unknown" {
				t.Fatalf("%+v %v", v, err)
			}
			_, _ = e.Generate(context.Background(), request())
			if calls.Load() != 1 {
				t.Fatal("uncertain write repeated")
			}
		})
	}
}

func TestConcurrentGenerationLocksBeforeSending(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	e := setup(t, func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		io.WriteString(w, `{"code":0,"data":{"project_id":"p","operator_id":"o"}}`)
	})
	finished := make(chan error, 1)
	go func() { _, err := e.Generate(context.Background(), request()); finished <- err }()
	<-entered
	_, err := e.Generate(context.Background(), request())
	close(release)
	if fault.Public(err).Code != "BUSY" {
		t.Fatalf("expected lock refusal: %v", err)
	}
	if err = <-finished; err != nil {
		t.Fatal(err)
	}
}

func TestNoAuthorizationNoNetwork(t *testing.T) {
	e := New(t.TempDir())
	in := request()
	in.Confirm = false
	_, err := e.Generate(context.Background(), in)
	if fault.Public(err).Code != "CONFIRM_REQUIRED" {
		t.Fatal(err)
	}
	jobs, _ := e.List()
	if len(jobs) != 0 {
		t.Fatal("unauthorized request created a job")
	}
}
