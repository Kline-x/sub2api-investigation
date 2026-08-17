package antigravity

import "testing"

func TestDefaultModels_ContainsNewAndLegacyImageModels(t *testing.T) {
	t.Parallel()

	models := DefaultModels()
	byID := make(map[string]ClaudeModel, len(models))
	for _, m := range models {
		byID[m.ID] = m
	}

	requiredIDs := []string{
		"claude-fable-5",
		"claude-opus-4-8",
		"claude-opus-4-6-thinking",
		"gemini-2.5-flash-image",
		"gemini-2.5-flash-image-preview",
		"gemini-3.1-flash-image",
		"gemini-3.1-flash-image-preview",
		"gemini-3-pro-image", // legacy compatibility
		"gemini-3.6-flash",
		"gemini-3.6-flash-high",
		"gemini-3.6-flash-low",
		"gemini-3.6-flash-medium",
		"gemini-3.6-flash-tiered",
		// 定制：3.7 只登记 -tiered（其余 3.7 命名上游 404，见 claude_types.go 注释）
		"gemini-3.7-flash-tiered",
	}

	for _, id := range requiredIDs {
		if _, ok := byID[id]; !ok {
			t.Fatalf("expected model %q to be exposed in DefaultModels", id)
		}
	}
}

// TestDefaultModels_不暴露上游404的37变体 锁住「只补 -tiered」这个决定。
// 2026-08-17 实测：这些名字打上游一律 404 Requested entity was not found，
// 放进目录等于把 404 摆到面板上让人选。合并上游若看到它们被加进来，先复测再说。
func TestDefaultModels_不暴露上游404的37变体(t *testing.T) {
	t.Parallel()

	byID := make(map[string]struct{})
	for _, m := range DefaultModels() {
		byID[m.ID] = struct{}{}
	}

	for _, id := range []string{
		"gemini-3.7-flash",
		"gemini-3.7-flash-high",
		"gemini-3.7-flash-low",
		"gemini-3.7-flash-medium",
		"gemini-3.7-flash-extra-low",
		"gemini-3.7-flash-image",
		"gemini-3.7-flash-agent",
		"gemini-3.7-pro",
		"gemini-3.7-pro-tiered",
		"gemini-3.7-pro-high",
		"gemini-3.7-pro-low",
	} {
		if _, ok := byID[id]; ok {
			t.Fatalf("模型 %q 上游返回 404，不应出现在 DefaultModels 里", id)
		}
	}
}
