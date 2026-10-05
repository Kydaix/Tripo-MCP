package studio

// This is an explicit protocol snapshot, not a promise of account entitlement.
func Capabilities() any {
	return map[string]any{
		"verified_against": "Studio web application, 2026-10-05",
		"models":           map[string]string{"h3.1": HD31, "h3.0": "v3.0-20250812", "h2.5": "v2.5-20250123", "p2.0": P20, "p1.0": P10},
		"generation": map[string]any{
			"sources":           []string{"prompt", "image", "images (front,left,back,right)", "batch_images"},
			"faces":             map[string]any{"minimum": 500, "hd_triangle_standard_max": 1000000, "h3_1_triangle_ultra_max": 2000000, "hd_quad_max": 50000, "smart_poly_triangle_max": 20000, "smart_poly_quad_max": 10000, "p2_triangle_max": 50000, "p2_quad_max": 25000, "p1_max": 20000, "parts_min": 10000},
			"geometry_quality":  []string{"standard", "detailed"},
			"texture_quality":   map[string]int{"standard": 2048, "detailed": 4096, "extreme": 8192},
			"texture_alignment": []string{"original_image", "geometry"},
			"parts_level":       []string{"simple", "balanced", "detailed"},
			"visibility":        []string{"private", "shareable", "public"},
			"p2_variants":       []int{1, 2, 4},
			"controls":          []string{"quad", "smart_poly", "generate_parts", "no_texture", "no_pbr", "delight", "image_autofix", "negative_prompt", "t_pose", "symmetry", "variation_faces"},
		},
		"edit_operations": editFields,
		"credits":         map[string]any{"estimate_tool": "tripo_estimate", "actual_tool": "tripo_cost", "estimate_basis": "Barème Studio du 2026-10-05, non garanti", "actual_basis": "Historique lié à operator_id, jamais une différence de solde"},
		"import":          map[string]any{"use_original_uv_default": true, "warnings": ImportWarnings(false), "uv_guard": "UV GLB absents ou entièrement dégénérés refusés avant import ; contrôle avant texture des modèles importés, y compris avec parts explicites.", "inspection_limits": "UV GLB non compressés ; pas de test exhaustif des chevauchements. Meshopt/Draco/FBX : non vérifiés. Textures PNG/JPEG intégrées jusqu'à 4K ; 8K/WebP : non vérifiés. Dimensions via positions ou bornes glTF, sans animation ni morph."},
		"export":          map[string]any{"formats": []string{"glb", "fbx", "obj", "stl", "3mf", "usdz"}, "texture_sizes": []int{512, 1024, 2048, 4096, 8192}, "packaging": []string{"embedded", "zip"}, "fbx_presets": []string{"blender", "3dsmax", "mixamo"}, "controls": []string{"pack_uv", "vertex_colors", "with_animation", "animations", "animate_in_place", "bake_animation", "bake_frame"}},
		"limitations":     []string{"Disponibilité et crédits contrôlés par Studio selon l'abonnement.", "Les opérations edit modifient le projet courant ; une ancienne version source est refusée.", "Les éditeurs interactifs de peinture, UV et sélection 3D ne sont pas reproduits.", "Validation réelle : texte HD → GLB/export FBX ; texte P2.0 → FBX quad → texture 8K → export GLB 8K. Les autres combinaisons sont vérifiées par contrats HTTP."},
	}
}
