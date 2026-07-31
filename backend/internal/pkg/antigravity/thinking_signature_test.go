//go:build unit

package antigravity

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// Anthropic 线上格式里 thinking 块必须同时带 thinking 和 signature：
// {"type":"thinking","thinking":"","signature":""}
// 两个字段在结构体上都有 omitempty，空值会被丢掉，严格客户端(grok-shell 等
// Rust/serde 实现)直接报 `missing field signature`，整条响应作废。
// 2026-07-31 实战踩坑：实抓到的 content_block_start 是
// {"content_block":{"thinking":"","type":"thinking"},...}，缺 signature。
func TestClaudeContentItemThinkingAlwaysCarriesSignature(t *testing.T) {
	for _, tc := range []struct {
		name string
		item ClaudeContentItem
	}{
		{name: "全空", item: ClaudeContentItem{Type: "thinking"}},
		{name: "有思考无签名", item: ClaudeContentItem{Type: "thinking", Thinking: "推理"}},
		{name: "两者都有", item: ClaudeContentItem{Type: "thinking", Thinking: "推理", Signature: "sig"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(tc.item)
			require.NoError(t, err)

			var decoded map[string]any
			require.NoError(t, json.Unmarshal(raw, &decoded))
			require.Containsf(t, decoded, "thinking", "thinking 项缺 thinking;JSON=%s", raw)
			require.Containsf(t, decoded, "signature", "thinking 项缺 signature(客户端报 missing field `signature`);JSON=%s", raw)
			require.Equal(t, tc.item.Signature, decoded["signature"])
		})
	}
}

// 非 thinking 类型不受影响：text 项不应多出 signature/thinking。
func TestClaudeContentItemTextUnaffected(t *testing.T) {
	raw, err := json.Marshal(ClaudeContentItem{Type: "text", Text: "hi"})
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(raw, &decoded))
	require.Contains(t, decoded, "text")
	require.NotContains(t, decoded, "signature")
	require.NotContains(t, decoded, "thinking")
}

// 流式路径：thinking 块的 content_block_start 必须带 signature。
func TestStreamingThinkingBlockStartCarriesSignature(t *testing.T) {
	p := NewStreamingProcessor("gemini-3.1-pro-high")
	out := string(p.processThinking("推理内容", ""))

	require.Contains(t, out, "content_block_start")
	require.Containsf(t, out, `"signature":""`,
		"流式 thinking 的 content_block_start 缺 signature;输出=%s", out)
}
