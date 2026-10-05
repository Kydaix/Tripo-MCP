package studio

import (
	"context"
	"fmt"
	"github.com/Kydaix/Tripo-MCP/internal/fault"
	"math"
	"time"
)

// Estimates are a dated snapshot of Studio's displayed standard tariff, not a
// binding quote or a wallet delta. Promotions and account quotas may differ.
type Estimate struct {
	Status string   `json:"status"`
	Amount *float64 `json:"amount,omitempty"`
	Source string   `json:"source"`
	AsOf   string   `json:"as_of"`
	Note   string   `json:"note"`
}

type CreditRecord struct {
	Amount    float64 `json:"amount"`
	Direction string  `json:"direction"`
	Status    string  `json:"status"`
	Type      string  `json:"txn_type"`
	Subtype   string  `json:"sub_type"`
}

type Usage struct {
	Status    string         `json:"status"`
	Charged   *float64       `json:"charged,omitempty"`
	Refunded  *float64       `json:"refunded,omitempty"`
	Net       *float64       `json:"net,omitempty"`
	Records   []CreditRecord `json:"records,omitempty"`
	CheckedAt time.Time      `json:"checked_at"`
	Complete  bool           `json:"history_complete"`
	Note      string         `json:"note,omitempty"`
}

type Credits struct {
	Estimate *Estimate `json:"estimate,omitempty"`
	Actual   *Usage    `json:"actual,omitempty"`
}

func estimate(amount float64) *Estimate {
	return &Estimate{Status: "estimated", Amount: &amount, Source: "studio_web_tariff", AsOf: "2026-10-05", Note: "Barème standard observé dans Studio ; estimation, hors quotas gratuits/remises. La facturation réelle vient de l'historique par opération."}
}
func unknownEstimate(note string) *Estimate {
	return &Estimate{Status: "unknown", Source: "studio_web_tariff", AsOf: "2026-10-05", Note: note}
}
func EstimateGeneration(in GenerateInput) *Estimate {
	n, err := in.Normalize()
	if err != nil {
		return unknownEstimate("Paramètres non valides.")
	}
	amount := 15.0
	switch n.Model {
	case P20:
		if n.Count > 1 {
			return unknownEstimate("Tarif des variantes P2.0 non vérifié ; consulter Studio avant soumission.")
		}
		amount = 100
	case P10:
		amount = 35
		if *n.Quad {
			amount += 5
		}
	default:
		if !n.NoTexture {
			amount = 25
			if !n.NoPBR {
				amount += 5
			}
			amount += textureExtra(n.TextureQuality)
		}
		if *n.Quad {
			amount += 5
		}
		if n.GenerateParts {
			amount += 30
		}
		if n.SmartPoly {
			amount += 20
		}
		if n.Model == HD31 && n.GeometryQuality == "detailed" {
			amount += 15
		}
	}
	if len(n.BatchImages) > 0 {
		amount *= float64(len(n.BatchImages))
	}
	return estimate(amount)
}
func textureExtra(quality string) float64 {
	switch quality {
	case "detailed":
		return 10
	case "extreme":
		return 20
	}
	return 0
}
func EstimateEdit(in EditInput) *Estimate {
	n, err := in.Normalize()
	if err != nil {
		return unknownEstimate("Paramètres non valides.")
	}
	amount := 0.0
	switch n.Operation {
	case "texture":
		amount = 10 + textureExtra(n.TextureQuality)
		if n.StyleImage != "" {
			amount += 5
		}
	case "upscale":
		amount = 10
		if n.TextureQuality == "extreme" {
			amount = 20
		}
	case "pbr":
		amount = 5
	case "remesh":
		amount = 5
		if n.Quad {
			amount = 10
		}
		if n.SmartPoly {
			amount += 30
		}
	case "segment":
		amount = 40
	case "rig":
		amount = 20
	default:
		return unknownEstimate("Tarif de cette opération non vérifié ; consulter Studio avant soumission.")
	}
	return estimate(amount)
}
func EstimateExport(_ ExportOptions) *Estimate {
	return unknownEstimate("Le tarif d'export dépend du propriétaire et des droits Studio ; aucun prix garanti par le client.")
}
func EstimateImport() *Estimate {
	return unknownEstimate("Le contrat d'import ne fournit pas de devis ; aucun montant supposé.")
}

// CreditUsage joins Studio's ledger on the exact operator ID. Never attribute
// a global wallet delta to a task, even if the agent believes it runs alone.
func (c *Client) CreditUsage(ctx context.Context, operator string) (*Usage, error) {
	u := &Usage{Status: "not_found", CheckedAt: time.Now().UTC()}
	seen := map[string]bool{}
	if operator == "" {
		u.Status = "unknown"
		u.Note = "Aucun reçu d'opération ; coût non attribuable."
		return u, nil
	}
	for page := 1; page <= 20; page++ {
		var data struct {
			Records *[]struct {
				CreditRecord
				Operator string   `json:"operator_id"`
				Amount   *float64 `json:"amount"`
				Created  string   `json:"create_time"`
			} `json:"records"`
			More *bool `json:"have_more"`
		}
		if err := c.request(ctx, "GET", fmt.Sprintf("/v2/studio/txn/records?page_num=%d&page_size=100", page), nil, &data, false); err != nil {
			return nil, err
		}
		if data.Records == nil || data.More == nil {
			return nil, invalidLedger()
		}
		for _, r := range *data.Records {
			if r.Operator != operator {
				continue
			}
			if r.Amount == nil || math.IsNaN(*r.Amount) || math.IsInf(*r.Amount, 0) || *r.Amount < 0 || !oneOf(r.Direction, "add", "sub") || !oneOf(r.Status, "fulfilled", "pending", "cancelled", "expired", "failed") {
				return nil, invalidLedger()
			}
			r.CreditRecord.Amount = *r.Amount
			key := fmt.Sprintf("%q/%g/%s/%s/%s/%s", r.Created, *r.Amount, r.Direction, r.Status, r.Type, r.Subtype)
			if seen[key] {
				continue
			}
			seen[key] = true
			u.Records = append(u.Records, r.CreditRecord)
		}
		if !*data.More {
			u.Complete = true
			break
		}
		if len(*data.Records) == 0 {
			return nil, invalidLedger()
		}
	}
	if !u.Complete {
		u.Status = "incomplete"
		u.Note = "Historique limité à 2000 lignes ; aucun total annoncé."
		return u, nil
	}
	if len(u.Records) == 0 {
		u.Note = "Aucune ligne trouvée ; cela ne prouve pas que l'opération est gratuite."
		return u, nil
	}
	charged, refunded := 0.0, 0.0
	u.Status = "recorded"
	for _, r := range u.Records {
		if r.Status == "pending" {
			u.Status = "pending"
		}
		if r.Status != "fulfilled" {
			continue
		}
		if r.Direction == "sub" {
			charged += r.Amount
		} else {
			refunded += r.Amount
		}
	}
	net := charged - refunded
	u.Charged, u.Refunded, u.Net = &charged, &refunded, &net
	u.Note = "Montants de l'historique Studio pour cette opération, à la date de lecture ; les réservations pending ne sont pas des débits confirmés."
	return u, nil
}
func invalidLedger() error {
	return fault.New("PROTOCOL_CHANGED", "Historique de crédits Studio non reconnu ; aucun coût déduit du solde global.")
}
