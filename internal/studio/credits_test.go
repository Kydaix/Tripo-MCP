package studio

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTariffSnapshotDistinguishesEstimateFromCharge(t *testing.T) {
	for _, tc := range []struct {
		in     GenerateInput
		amount float64
	}{
		{GenerateInput{Prompt: "owl"}, 30},
		{GenerateInput{Prompt: "owl", GeometryQuality: "detailed"}, 45},
		{GenerateInput{Prompt: "owl", TextureQuality: "extreme", Quad: Bool(true)}, 55},
		{GenerateInput{Prompt: "owl", NoTexture: true, GenerateParts: true}, 45},
		{GenerateInput{Prompt: "owl", Model: "p2.0"}, 100},
		{GenerateInput{BatchImages: []string{"a", "b"}}, 60},
	} {
		q := EstimateGeneration(tc.in)
		if q.Amount == nil || *q.Amount != tc.amount || q.Status != "estimated" || q.AsOf == "" {
			t.Fatalf("%+v => %+v", tc.in, q)
		}
	}
	q := EstimateEdit(EditInput{Operation: "texture", Prompt: "bronze", TextureQuality: "detailed"})
	if q.Amount == nil || *q.Amount != 20 {
		t.Fatal(q)
	}
	for _, q := range []*Estimate{EstimateImport(), EstimateExport(ExportOptions{}), EstimateGeneration(GenerateInput{Prompt: "owl", Model: "p2.0", Count: 4})} {
		if q.Amount != nil || q.Status != "unknown" {
			t.Fatal("invented cost")
		}
	}
}

func TestLedgerMatchesExactOperationAcrossPagesAndRefunds(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "GET" || r.URL.Path != "/v2/studio/txn/records" || r.URL.Query().Get("page_size") != "100" {
			t.Error("not read-only ledger")
		}
		rows := []map[string]any{}
		more := true
		if r.URL.Query().Get("page_num") == "1" {
			rows = append(rows, map[string]any{"operator_id": "other", "amount": 85, "direction": "sub", "status": "fulfilled"})
		} else {
			more = false
			for _, item := range []struct {
				amount            int
				direction, status string
			}{{20, "sub", "fulfilled"}, {5, "add", "fulfilled"}, {100, "sub", "cancelled"}, {10, "sub", "pending"}} {
				rows = append(rows, map[string]any{"operator_id": "wanted", "amount": item.amount, "direction": item.direction, "status": item.status, "txn_type": "charge"})
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"records": rows, "have_more": more}})
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	u, err := c.CreditUsage(context.Background(), "wanted")
	if err != nil || u.Net == nil || *u.Net != 15 || *u.Charged != 20 || *u.Refunded != 5 || u.Status != "pending" || !u.Complete || len(u.Records) != 4 || calls != 2 {
		t.Fatalf("%+v %v calls=%d", u, err, calls)
	}
	b, _ := json.Marshal(u)
	var out map[string]any
	json.Unmarshal(b, &out)
	for _, r := range out["records"].([]any) {
		if _, ok := r.(map[string]any)["operator_id"]; ok {
			t.Fatal("unnecessary remote identity in ledger output")
		}
	}
}

func TestLedgerNeverTurnsMissingOrInvalidDataIntoFree(t *testing.T) {
	for _, tc := range []struct {
		body   string
		status string
		fail   bool
	}{
		{`{"records":[],"have_more":false}`, "not_found", false},
		{`{}`, "", true},
		{`{"records":[{"operator_id":"wanted","direction":"sub","status":"fulfilled"}],"have_more":false}`, "", true},
		{`{"records":[{"operator_id":"wanted","amount":-20,"direction":"sub","status":"fulfilled"}],"have_more":false}`, "", true},
		{`{"records":[{"operator_id":"wanted","amount":20,"direction":"sub","status":"new"}],"have_more":false}`, "", true},
		{`{"records":[{"operator_id":"wanted","amount":20,"direction":"sub","status":"fulfilled"}],"have_more":true}`, "incomplete", false},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprintf(w, `{"code":0,"data":%s}`, tc.body) }))
		c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
		u, err := c.CreditUsage(context.Background(), "wanted")
		srv.Close()
		if (err != nil) != tc.fail {
			t.Fatal(err)
		}
		if !tc.fail && (u.Status != tc.status || u.Net != nil) {
			t.Fatalf("%+v", u)
		}
	}
}
