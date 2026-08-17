//go:build unit

package dto

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// 定制：「编辑账号 → 模型限制」显示的是**有效**映射（存的 + 运行时默认透传），
// 否则像 gemini-3.7-flash-tiered 这类靠本地目录补进来的模型在该列表里看不到，
// 会被误判成「白名单会拦」（实际网关放行）。

func TestAccountFromServiceShallow_Antigravity模型限制包含默认透传(t *testing.T) {
	account := &service.Account{
		ID:       76,
		Platform: service.PlatformAntigravity,
		Credentials: map[string]any{
			"model_mapping": map[string]any{
				"gemini-3.6-flash-tiered": "gemini-3.6-flash-tiered",
				"gemini-pro-agent":        "gemini-pro-agent",
			},
		},
	}

	out := AccountFromServiceShallow(account)
	require.NotNil(t, out)

	mapping, ok := out.Credentials["model_mapping"].(map[string]any)
	require.True(t, ok, "model_mapping 应仍是对象")

	// 账号自己配的条目保留
	require.Equal(t, "gemini-pro-agent", mapping["gemini-pro-agent"])
	// 默认透传补进来的条目也要可见
	require.Equal(t, "gemini-3.7-flash-tiered", mapping["gemini-3.7-flash-tiered"],
		"gemini-3.7-flash-tiered 应出现在模型限制列表里")
}

func TestAccountFromServiceShallow_未配模型限制时不摊开默认清单(t *testing.T) {
	account := &service.Account{
		ID:          77,
		Platform:    service.PlatformAntigravity,
		Credentials: map[string]any{"model_mapping": map[string]any{}},
	}

	out := AccountFromServiceShallow(account)
	require.NotNil(t, out)

	mapping, _ := out.Credentials["model_mapping"].(map[string]any)
	require.Empty(t, mapping, "mapping 为空表示不限制，不应被默认清单填满")
}

func TestAccountFromServiceShallow_非Antigravity平台不受影响(t *testing.T) {
	account := &service.Account{
		ID:       78,
		Platform: service.PlatformOpenAI,
		Credentials: map[string]any{
			"model_mapping": map[string]any{"gpt-5.4": "gpt-5.4"},
		},
	}

	out := AccountFromServiceShallow(account)
	require.NotNil(t, out)

	mapping, _ := out.Credentials["model_mapping"].(map[string]any)
	require.Len(t, mapping, 1)
	require.Equal(t, "gpt-5.4", mapping["gpt-5.4"])
}
