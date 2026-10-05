package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"github.com/Kydaix/Tripo-MCP/internal/studio"
	"io"
	"os"
	"strconv"
	"strings"
)

type stringsFlag struct{ target *[]string }

func (s stringsFlag) String() string     { return strings.Join(*s.target, ",") }
func (s stringsFlag) Set(v string) error { *s.target = append(*s.target, v); return nil }

type optionalBool struct{ target **bool }

func (s optionalBool) String() string {
	if *s.target == nil {
		return ""
	}
	return strconv.FormatBool(**s.target)
}
func (s optionalBool) IsBoolFlag() bool { return true }
func (s optionalBool) Set(v string) error {
	b, err := strconv.ParseBool(v)
	if err == nil {
		*s.target = &b
	}
	return err
}

func generationFlags(f *flag.FlagSet, p *studio.GenerateInput) {
	f.StringVar(&p.Prompt, "prompt", "", "description")
	f.StringVar(&p.Image, "image", "", "image locale")
	f.Var(stringsFlag{&p.Images}, "view", "vue locale ; répéter quatre fois : avant, gauche, arrière, droite")
	f.Var(stringsFlag{&p.BatchImages}, "batch-image", "image indépendante ; répétable")
	f.StringVar(&p.Model, "model", "", "h3.1, h3.0, h2.5, p2.0 ou p1.0")
	f.IntVar(&p.Faces, "faces", 0, "budget de polygones (défaut selon modèle)")
	f.BoolVar(&p.NoTexture, "no-texture", false, "géométrie sans texture")
	f.BoolVar(&p.NoPBR, "no-pbr", false, "désactiver PBR")
	f.StringVar(&p.Visibility, "visibility", "", "private, shareable ou public")
	f.Var(optionalBool{&p.Quad}, "quad", "quadrangles ; --quad=false pour triangles")
	f.BoolVar(&p.SmartPoly, "smart-poly", false, "réduction Smart Poly HD")
	f.BoolVar(&p.GenerateParts, "generate-parts", false, "générer en parties (nécessite --no-texture)")
	f.StringVar(&p.PartsLevel, "parts-level", "", "simple, balanced ou detailed")
	f.StringVar(&p.GeometryQuality, "geometry-quality", "", "standard ou detailed (Ultra)")
	f.StringVar(&p.TextureQuality, "texture-quality", "", "standard (2K), detailed (4K), extreme (8K)")
	f.StringVar(&p.TextureAlignment, "texture-alignment", "", "original_image ou geometry")
	f.BoolVar(&p.Delight, "delight", false, "retirer l'éclairage")
	f.BoolVar(&p.ImageAutofix, "image-autofix", false, "AI Complete")
	f.StringVar(&p.NegativePrompt, "negative-prompt", "", "éléments à éviter")
	f.BoolVar(&p.TPose, "t-pose", false, "pose en T")
	f.IntVar(&p.Count, "count", 0, "variantes P2.0 : 1, 2 ou 4")
	f.Func("variation-faces", "budgets individuels séparés par virgules", func(v string) error {
		for _, part := range strings.Split(v, ",") {
			n, err := strconv.Atoi(strings.TrimSpace(part))
			if err != nil {
				return err
			}
			p.VariationFaces = append(p.VariationFaces, n)
		}
		return nil
	})
	f.Var(optionalBool{&p.Symmetry}, "symmetry", "symétrie P2.0 ; détection automatique si omis")
}
func editFlags(f *flag.FlagSet, p *studio.EditInput) {
	f.StringVar(&p.Operation, "operation", "", "texture, upscale, pbr, remesh, segment, fill, complete, rig ou animate")
	f.StringVar(&p.Prompt, "prompt", "", "description de texture")
	f.StringVar(&p.Image, "image", "", "image de référence")
	f.Var(stringsFlag{&p.Images}, "view", "vue de texture ; répéter quatre fois")
	f.StringVar(&p.StyleImage, "style-image", "", "image de style")
	f.StringVar(&p.TextureQuality, "texture-quality", "", "standard, detailed ou extreme")
	f.StringVar(&p.TextureAlignment, "texture-alignment", "", "original_image ou geometry")
	f.Var(optionalBool{&p.Delight}, "delight", "retirer l'éclairage ; défaut true")
	f.Var(stringsFlag{&p.Parts}, "part", "nom de partie ciblée ; répétable")
	f.IntVar(&p.Faces, "faces", 0, "budget de retopologie")
	f.BoolVar(&p.Quad, "quad", false, "retopologie en quadrangles")
	f.BoolVar(&p.SmartPoly, "smart-poly", false, "retopologie Smart Poly")
	f.Var(optionalBool{&p.Bake}, "bake", "reprojection des textures ; défaut true")
	f.StringVar(&p.PartsLevel, "parts-level", "", "simple, balanced ou detailed")
	f.StringVar(&p.RigType, "rig-type", "", "type de rig")
	f.StringVar(&p.Skeleton, "skeleton", "", "mixamo, actorcore, unreal, unity ou vrm")
	f.Var(stringsFlag{&p.Animations}, "animation", "nom exact d'animation Studio ; répétable")
	f.StringVar(&p.MotionAssetID, "motion-asset-id", "", "mouvement Studio existant")
}
func exportFlags(f *flag.FlagSet, p *studio.ExportOptions) {
	f.StringVar(&p.Format, "format", "glb", "glb, fbx, obj, stl, 3mf ou usdz")
	f.IntVar(&p.TextureSize, "texture-size", 0, "512, 1024, 2048, 4096 ou 8192")
	f.StringVar(&p.Packaging, "packaging", "", "embedded ou zip")
	f.BoolVar(&p.PackUV, "pack-uv", false, "regrouper les UV")
	f.BoolVar(&p.VertexColors, "vertex-colors", false, "couleurs de sommets OBJ")
	f.StringVar(&p.FBXPreset, "fbx-preset", "", "blender, 3dsmax ou mixamo")
	f.BoolVar(&p.WithAnimation, "with-animation", false, "inclure les animations")
	f.Var(stringsFlag{&p.Animations}, "animation", "animation exportée ; répétable")
	f.BoolVar(&p.AnimateInPlace, "animate-in-place", false, "animation sur place")
	f.BoolVar(&p.BakeAnimation, "bake-animation", false, "figer une pose")
	f.IntVar(&p.BakeFrame, "bake-frame", 0, "frame de la pose")
}
func readParams(path string, value any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil {
		return err
	}
	if len(b) > 1<<20 || !bytes.HasPrefix(bytes.TrimSpace(b), []byte("{")) {
		return os.ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(value); err != nil {
		return err
	}
	var extra any
	if err = decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return os.ErrInvalid
		}
		return err
	}
	return nil
}
