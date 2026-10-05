package engine

import (
	"context"
	"github.com/Kydaix/Tripo-MCP/internal/fault"
	"github.com/Kydaix/Tripo-MCP/internal/local"
	"github.com/Kydaix/Tripo-MCP/internal/studio"
	"time"
)

// Costs refreshes existing receipts only. An unavailable ledger never changes
// the remote task state and never causes an operation to be submitted again.
func (e *Engine) Costs(ctx context.Context, id string) (View, error) {
	p, err := e.jobPath(id)
	if err != nil {
		return View{}, err
	}
	unlock, err := local.Lock(p + ".lock")
	if err != nil {
		return View{}, err
	}
	defer unlock()
	j, err := e.read(id)
	if err != nil {
		return View{}, err
	}
	s, c, err := e.client(ctx)
	if err != nil {
		return j.View(), err
	}
	if s.Account != j.Account {
		return j.View(), fault.New("ACCOUNT_CHANGED", "Cette tâche appartient à un autre compte.")
	}
	if len(j.Variants) > 0 {
		for i := range j.Variants {
			e.refreshCredits(ctx, c, &j.Variants[i])
		}
	} else {
		e.refreshCredits(ctx, c, &j)
	}
	if err = e.save(&j); err != nil {
		return j.View(), err
	}
	return j.View(), nil
}
func (e *Engine) refreshCredits(ctx context.Context, c *studio.Client, j *Job) {
	if j.Credits == nil {
		j.Credits = &studio.Credits{}
	}
	readCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	u, err := c.CreditUsage(readCtx, j.Receipt.OperatorID)
	if err != nil {
		u = &studio.Usage{Status: "unavailable", CheckedAt: time.Now().UTC(), Note: fault.Public(err).Message}
	}
	j.Credits.Actual = u
}

type EstimateRequest struct {
	Generate *studio.GenerateInput `json:"generate,omitempty"`
	Edit     *studio.EditInput     `json:"edit,omitempty"`
	Export   *studio.ExportOptions `json:"export,omitempty"`
	Import   *studio.ImportInput   `json:"import,omitempty"`
}

func Estimate(in EstimateRequest) (any, error) {
	count := 0
	for _, v := range []bool{in.Generate != nil, in.Edit != nil, in.Export != nil, in.Import != nil} {
		if v {
			count++
		}
	}
	if count != 1 {
		return nil, fault.New("INVALID_ARGUMENT", "Fournir exactement un objet generate, edit, export ou import.")
	}
	var params any
	var cost *studio.Estimate
	var err error
	switch {
	case in.Generate != nil:
		params, err = in.Generate.Normalize()
		cost = studio.EstimateGeneration(*in.Generate)
	case in.Edit != nil:
		params, err = in.Edit.Normalize()
		cost = studio.EstimateEdit(*in.Edit)
	case in.Export != nil:
		params, err = in.Export.Normalize()
		cost = studio.EstimateExport(*in.Export)
	case in.Import != nil:
		params, err = in.Import.Normalize()
		cost = studio.EstimateImport()
	}
	if err != nil {
		return nil, err
	}
	preview := map[string]any{"parameters": params, "credits": &studio.Credits{Estimate: cost}}
	if in.Import != nil {
		p := params.(studio.ImportInput)
		inspection := studio.InspectModel(p.ModelFile, false)
		preview["inspection"] = inspection
		preview["warnings"] = studio.ImportWarnings(*p.UseOriginalUV)
		if err = studio.CheckTextureUV(inspection); err != nil {
			return preview, err
		}
	}
	return preview, nil
}
