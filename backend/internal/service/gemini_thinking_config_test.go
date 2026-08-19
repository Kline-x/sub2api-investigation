//go:build unit

package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// gemini-* 模型走 convertClaudeMessagesToGeminiGenerateContent 这条转换器（claude-* 走 antigravity
// 包的 TransformClaudeToGeminiWithOptions）。该转换器原先只认 max_tokens/temperature/top_p/
// stop_sequences，thinking 被整个丢掉，导致 OpenAI 协议入口打 gemini-* 永远拿不到思考过程。
// 这里锁住映射语义，避免合并上游时被改回去。
func TestConvertClaudeMessagesToGeminiGenerateContent_ThinkingConfig(t *testing.T) {
	generationConfig := func(t *testing.T, claudeBody string) map[string]any {
		t.Helper()
		out, err := convertClaudeMessagesToGeminiGenerateContent([]byte(claudeBody))
		require.NoError(t, err)
		var parsed map[string]any
		require.NoError(t, json.Unmarshal(out, &parsed))
		gc, _ := parsed["generationConfig"].(map[string]any)
		return gc
	}

	t.Run("thinking enabled 生成 includeThoughts", func(t *testing.T) {
		gc := generationConfig(t, `{"model":"gemini-3.7-flash-tiered","max_tokens":16384,
			"thinking":{"type":"enabled","budget_tokens":8192},
			"messages":[{"role":"user","content":"hi"}]}`)

		tc, ok := gc["thinkingConfig"].(map[string]any)
		require.True(t, ok, "缺少 thinkingConfig —— 不带这个开关上游一律不返回 thought part")
		require.Equal(t, true, tc["includeThoughts"])
		require.Equal(t, float64(8192), tc["thinkingBudget"])
		require.Equal(t, float64(16384), gc["maxOutputTokens"])
	})

	t.Run("adaptive 同样启用，未给预算时用动态 -1", func(t *testing.T) {
		gc := generationConfig(t, `{"model":"gemini-3.6-flash-high","max_tokens":4096,
			"thinking":{"type":"adaptive"},
			"messages":[{"role":"user","content":"hi"}]}`)

		tc, ok := gc["thinkingConfig"].(map[string]any)
		require.True(t, ok)
		require.Equal(t, true, tc["includeThoughts"])
		require.Equal(t, float64(-1), tc["thinkingBudget"])
	})

	t.Run("max_tokens 不大于 budget 时自动抬高", func(t *testing.T) {
		// 上游要求 max_tokens > budget_tokens，否则 400。
		gc := generationConfig(t, `{"model":"gemini-3.6-flash-high","max_tokens":4096,
			"thinking":{"type":"enabled","budget_tokens":8192},
			"messages":[{"role":"user","content":"hi"}]}`)

		require.Equal(t, float64(8192+1000), gc["maxOutputTokens"])
	})

	t.Run("gemini-2.5-flash 预算封顶", func(t *testing.T) {
		gc := generationConfig(t, `{"model":"gemini-2.5-flash","max_tokens":65536,
			"thinking":{"type":"enabled","budget_tokens":99999},
			"messages":[{"role":"user","content":"hi"}]}`)

		tc := gc["thinkingConfig"].(map[string]any)
		require.Equal(t, float64(24576), tc["thinkingBudget"])
	})

	t.Run("thinking disabled 或缺失时不注入", func(t *testing.T) {
		for _, body := range []string{
			`{"model":"gemini-3.6-flash-high","max_tokens":1024,"thinking":{"type":"disabled"},"messages":[{"role":"user","content":"hi"}]}`,
			`{"model":"gemini-3.6-flash-high","max_tokens":1024,"messages":[{"role":"user","content":"hi"}]}`,
		} {
			gc := generationConfig(t, body)
			require.NotContains(t, gc, "thinkingConfig")
		}
	})
}
