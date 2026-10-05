package engine

import (
	"github.com/Kydaix/Tripo-MCP/internal/studio"
	"math"
)

func inspectArtifact(j *Job, path string) {
	j.Inspection = studio.InspectModel(path, true)
	if j.ImportInfo == nil || j.ImportInfo.Source == nil {
		return
	}
	a, b := j.ImportInfo.Source.Dimensions, j.Inspection.Dimensions
	if len(a) != 3 || len(b) != 3 {
		return
	}
	changed := false
	for i := range a {
		if math.Abs(a[i]-b[i]) > math.Max(1e-5, math.Abs(a[i])*0.01) {
			changed = true
		}
	}
	if changed {
		j.Inspection.Status = "warning"
		j.Inspection.Warnings = append(j.Inspection.Warnings, "Les dimensions diffèrent de celles du fichier importé de plus de 1 % ; vérifier l'échelle dans le logiciel cible.")
	}
}
