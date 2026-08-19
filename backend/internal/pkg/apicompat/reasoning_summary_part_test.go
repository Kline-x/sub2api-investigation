//go:build unit

package apicompat

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// codex 按 summary part 的生命周期渲染推理过程：
//
//	output_item.added(reasoning) → reasoning_summary_part.added
//	→ reasoning_summary_text.delta ×N → reasoning_summary_text.done → reasoning_summary_part.done
//	→ output_item.done
//
// 缺 part.added 时 delta 没有可写入的槽位，表现为「工具调用能显示、推理过程不显示」；
// text.done 不带全文时，按 done 渲染的客户端拿到空串。两处都踩过，这里锁死。
func TestAnthropicThinkingToResponsesReasoningLifecycle(t *testing.T) {
	idx := func(i int) *int { return &i }
	state := NewAnthropicEventToResponsesState()
	state.Model = "gemini-3.6-flash-high"

	var got []ResponsesStreamEvent
	for _, evt := range []AnthropicStreamEvent{
		{Type: "message_start"},
		{Type: "content_block_start", Index: idx(0), ContentBlock: &AnthropicContentBlock{Type: "thinking"}},
		{Type: "content_block_delta", Index: idx(0), Delta: &AnthropicDelta{Type: "thinking_delta", Thinking: "先"}},
		{Type: "content_block_delta", Index: idx(0), Delta: &AnthropicDelta{Type: "thinking_delta", Thinking: "想想"}},
		{Type: "content_block_stop", Index: idx(0)},
	} {
		e := evt
		got = append(got, AnthropicEventToResponsesEvents(&e, state)...)
	}

	var types []string
	for _, e := range got {
		types = append(types, e.Type)
	}
	require.Equal(t, []string{
		"response.created",
		"response.output_item.added",
		"response.reasoning_summary_part.added",
		"response.reasoning_summary_text.delta",
		"response.reasoning_summary_text.delta",
		"response.reasoning_summary_text.done",
		"response.reasoning_summary_part.done",
		"response.output_item.done",
	}, types)

	byType := func(t string) *ResponsesStreamEvent {
		for i := range got {
			if got[i].Type == t {
				return &got[i]
			}
		}
		return nil
	}

	added := byType("response.reasoning_summary_part.added")
	require.NotNil(t, added.Part)
	require.Equal(t, "summary_text", added.Part.Type)
	require.Equal(t, "", added.Part.Text)

	done := byType("response.reasoning_summary_text.done")
	require.Equal(t, "先想想", done.Text, "done 必须带累计全文，不能是空串")

	partDone := byType("response.reasoning_summary_part.done")
	require.NotNil(t, partDone.Part)
	require.Equal(t, "先想想", partDone.Part.Text)

	// 三个 reasoning 事件必须挂在同一个 item 上，否则客户端无法归组
	require.Equal(t, added.ItemID, done.ItemID)
	require.Equal(t, added.ItemID, partDone.ItemID)
	require.NotEmpty(t, added.ItemID)
}
