// Package studio implements the current Studio wire protocol, independent of CLI, MCP and browser UI.
package studio

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/Kydaix/Tripo-MCP/internal/auth"
	"github.com/Kydaix/Tripo-MCP/internal/fault"
)

const Origin = "https://studio.tripo3d.ai"
const API = "https://api.tripo3d.ai"
const maxJSON = 8 << 20

type Client struct {
	Session     auth.Session
	HTTP        *http.Client
	BaseURL     string
	Credentials func(context.Context) (auth.Session, error)
}

func New(s auth.Session) *Client {
	return &Client{Session: s, BaseURL: API, HTTP: &http.Client{Timeout: 90 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

// request never retries writes. A write without a valid receipt is explicitly uncertain.
func (c *Client) request(ctx context.Context, method, path string, body, out any, paid bool) error {
	var data []byte
	var err error
	s := c.Session
	if c.Credentials != nil {
		s, err = c.Credentials(ctx)
		if err != nil {
			return err
		}
	}
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, bytes.NewReader(data))
	if err != nil {
		return fault.New("INVALID_ARGUMENT", "Requête invalide.")
	}
	req.Header.Set("Authorization", "Bearer "+s.Token)
	req.Header.Set("X-Tripo-Device-Id", s.DeviceID)
	if s.Region != "" {
		req.Header.Set("X-Tripo-Region", s.Region)
	}
	req.Header.Set("Origin", Origin)
	req.Header.Set("Referer", Origin+"/")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Tripo-MCP")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return uncertain(paid, "NETWORK_ERROR", "Studio injoignable ou délai dépassé.")
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxJSON+1))
	if err != nil || len(b) > maxJSON {
		return uncertain(paid, "PROTOCOL_CHANGED", "Réponse Studio illisible ou trop volumineuse.")
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return fault.New("AUTH_EXPIRED", "Session Studio expirée ; lancer tripo-mcp login.")
	}
	if resp.StatusCode == http.StatusForbidden {
		return fault.New("ACCESS_DENIED", "Studio refuse la requête ; vérifier le compte et les éventuelles vérifications dans le navigateur.")
	}
	if resp.StatusCode >= 500 {
		return uncertain(paid, "HTTP_ERROR", "Service Studio temporairement indisponible.")
	}
	var envelope struct {
		Code *int            `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(b, &envelope) != nil || envelope.Code == nil {
		return uncertain(paid, "PROTOCOL_CHANGED", "Format de réponse Studio inconnu.")
	}
	if *envelope.Code != 0 {
		if *envelope.Code == 2010 {
			return fault.New("INSUFFICIENT_CREDITS", "Crédits Studio insuffisants.")
		}
		return fault.New("STUDIO_ERROR", fmt.Sprintf("Studio a refusé la requête (code %d, HTTP %d).", *envelope.Code, resp.StatusCode))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return uncertain(paid, "HTTP_ERROR", fmt.Sprintf("Studio a répondu HTTP %d.", resp.StatusCode))
	}
	if out != nil && json.Unmarshal(envelope.Data, out) != nil {
		return uncertain(paid, "PROTOCOL_CHANGED", "Structure de réponse Studio inconnue.")
	}
	return nil
}

func uncertain(paid bool, code, message string) error {
	if paid {
		return fault.New("OUTCOME_UNKNOWN", "Réception de la demande non confirmée. Ne pas relancer : vérifier la tâche et l'historique Studio.")
	}
	return fault.New(code, message)
}

type Account struct {
	Member struct {
		Type       string `json:"type"`
		ValidUntil string `json:"valid_until"`
	} `json:"member"`
	Wallet struct {
		TotalCredit    float64 `json:"total_credit"`
		ExpiringCredit float64 `json:"expiring_credit"`
	} `json:"wallet"`
}

func (c *Client) Account(ctx context.Context) (Account, error) {
	var a Account
	err := c.request(ctx, "GET", "/v2/studio/user/profile/payment", nil, &a, false)
	return a, err
}

type Receipt struct {
	ProjectID  string `json:"project_id"`
	OperatorID string `json:"operator_id"`
}
type Progress struct {
	Reason *struct {
		Code int `json:"code"`
	} `json:"reason,omitempty"`
	OperatorID string  `json:"operator_id"`
	Status     string  `json:"status"`
	Progress   float64 `json:"progress"`
	// URLs are kept internal, never included in tool output or the journal.
	ModelURL string          `json:"model_url,omitempty"`
	Output   json.RawMessage `json:"output,omitempty"`
}

func (c *Client) Progress(ctx context.Context, id string) (Progress, error) {
	var raw json.RawMessage
	if err := c.request(ctx, "POST", "/v2/studio/progress", map[string]any{"ids": []string{id}}, &raw, false); err != nil {
		return Progress{}, err
	}
	var items []Progress
	if json.Unmarshal(raw, &items) != nil {
		var wrapper struct {
			Tasks []Progress `json:"tasks"`
			Items []Progress `json:"items"`
		}
		if json.Unmarshal(raw, &wrapper) != nil {
			return Progress{}, fault.New("PROTOCOL_CHANGED", "Progression Studio illisible.")
		}
		items = append(wrapper.Tasks, wrapper.Items...)
	}
	for _, p := range items {
		if p.OperatorID == id {
			return p, nil
		}
	}
	return Progress{}, fault.New("PROGRESS_UNAVAILABLE", "La réponse ne contient pas cette opération ; réessayer la lecture plus tard.")
}

func (c *Client) Project(ctx context.Context, project, operator string) (map[string]any, error) {
	var data map[string]any
	q := url.Values{}
	if operator != "" {
		q.Set("operator_id", operator)
	}
	err := c.request(ctx, "GET", "/v2/studio/project/detail/v3/"+url.PathEscape(project)+"?"+q.Encode(), nil, &data, false)
	return data, err
}

// ExportURL resolves the result of an already completed export; it creates no export.
func (c *Client) ExportURL(ctx context.Context, operator string) (string, error) {
	var out struct {
		ModelURL string `json:"model_url"`
	}
	err := c.request(ctx, "POST", "/v2/studio/operation/download_with_name", map[string]string{"operator_id": operator, "file_name": "model"}, &out, false)
	return out.ModelURL, err
}

func IsUncertain(err error) bool {
	var e *fault.Error
	return errors.As(err, &e) && e.Code == "OUTCOME_UNKNOWN"
}

// AssetURL only accepts known model fields. It never mistakes a preview or another asset for the model.
func AssetURL(project map[string]any) string {
	operator, _ := project["operator"].(map[string]any)
	for _, obj := range []map[string]any{operator, project} {
		for _, key := range []string{"model_url", "pbr_model_url", "glb_model_url"} {
			if s, ok := obj[key].(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

// Studio can return FBX for quad topology. Do not relabel it as a GLB.
func ModelFormat(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fault.New("INVALID_ASSET", "Adresse de modèle invalide.")
	}
	ext := strings.ToLower(path.Ext(u.Path))
	switch ext {
	case ".glb":
		return "glb", nil
	case ".fbx":
		return "fbx", nil
	}
	return "", fault.New("ASSET_UNAVAILABLE", "Format natif non reconnu ; demander un export explicite.")
}

func Terminal(status string) bool {
	switch strings.ToLower(status) {
	case "success", "failed", "banned", "cancelled", "expired", "partial":
		return true
	}
	return false
}
