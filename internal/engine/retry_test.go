package engine

import (
	"context"
	"github.com/Kydaix/Tripo-MCP/internal/fault"
	"github.com/Kydaix/Tripo-MCP/internal/studio"
	"io"
	"net/http"
	"testing"
)

func TestPreSubmissionFailureCanResumeStableIDButConflictsStillFail(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		writes := 0
		e := setup(t, func(w http.ResponseWriter, r *http.Request) {
			writes++
			io.WriteString(w, `{"code":0,"data":{"project_id":"p","operator_id":"o"}}`)
		})
		// Derive the real fingerprint once, then seed a known pre-submit failure.
		v, err := e.Generate(context.Background(), request())
		if err != nil {
			t.Fatal(err)
		}
		j, err := e.read(v.ID)
		if err != nil {
			t.Fatal(err)
		}
		j.State = "failed"
		j.Receipt = studio.Receipt{}
		j.Error = &fault.Error{Code: "IMAGE_REJECTED", Message: "old audit"}
		j.Retryable = !legacy
		e.save(&j)
		writes = 0
		changed := request()
		changed.Prompt = "different"
		if _, err = e.Generate(context.Background(), changed); fault.Public(err).Code != "REQUEST_CONFLICT" || writes != 0 {
			t.Fatal("conflict resumed")
		}
		restart := New(e.Root)
		restart.Client = e.Client
		v, err = restart.Generate(context.Background(), request())
		if err != nil || writes != 1 || v.State != "submitted" || v.Retryable || v.Error != nil {
			t.Fatalf("%+v %v writes=%d", v, err, writes)
		}
		_, err = restart.Generate(context.Background(), request())
		if err != nil || writes != 1 {
			t.Fatal("duplicate charge")
		}
	}
}

func TestOnlyProvenPreSubmissionJobsAreRetryable(t *testing.T) {
	for _, tc := range []struct {
		job  Job
		want bool
	}{
		{Job{State: "preparing"}, true},
		{Job{State: "failed", Retryable: true}, true},
		{Job{State: "failed", Error: &fault.Error{Code: "UPLOAD_FAILED"}}, true},
		{Job{State: "failed", Error: &fault.Error{Code: "STUDIO_ERROR"}}, false},
		{Job{State: "outcome_unknown", Retryable: true}, false},
		{Job{State: "failed", Retryable: true, Receipt: studio.Receipt{OperatorID: "o"}}, false},
		{Job{State: "failed", Retryable: true, Variants: []Job{{}}}, false},
	} {
		if tc.job.canResume() != tc.want {
			t.Fatalf("%+v", tc.job)
		}
	}
}

func TestCostFailureKeepsSuccessfulJobAndNeverSubmits(t *testing.T) {
	e := setup(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/v2/studio/txn/records" {
			t.Error("unexpected mutation")
		}
		w.WriteHeader(503)
	})
	seedSource(t, e)
	v, err := e.Costs(context.Background(), "source")
	if err != nil || v.State != "success" || v.Credits.Actual.Status != "unavailable" || v.Credits.Actual.Net != nil {
		t.Fatalf("%+v %v", v, err)
	}
}
