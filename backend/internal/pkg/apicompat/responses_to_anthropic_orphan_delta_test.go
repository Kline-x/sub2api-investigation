package apicompat

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// assertAnthropicBlockEventOrder 校验事件序列的块级不变量：
// content_block_delta / content_block_stop 引用的 index 必须先被 content_block_start 声明过。
// Claude Code（Anthropic SDK）在客户端累积流事件时依赖该不变量，违反即抛 "content block not found"。
func assertAnthropicBlockEventOrder(t *testing.T, events []AnthropicStreamEvent) {
	t.Helper()
	started := map[int]bool{}
	for _, e := range events {
		switch e.Type {
		case "content_block_start":
			require.NotNil(t, e.Index)
			started[*e.Index] = true
		case "content_block_delta", "content_block_stop":
			require.NotNil(t, e.Index)
			require.True(t, started[*e.Index],
				"%s references index %d without a preceding content_block_start", e.Type, *e.Index)
		}
	}
}

// 上游把 output_item.done 排在 function_call_arguments.done 之前时，
// arguments.done 不得向已关闭（或从未打开）的 index 发 delta。
func TestResToAnthFuncArgsDone_AfterItemDoneMustNotEmitOrphanDelta(t *testing.T) {
	state := NewResponsesEventToAnthropicState()

	var all []AnthropicStreamEvent
	sequence := []*ResponsesStreamEvent{
		{Type: "response.created", Response: &ResponsesResponse{ID: "resp_1", Model: "grok-4.5"}},
		{Type: "response.output_item.added", OutputIndex: 0, Item: &ResponsesOutput{Type: "function_call", Name: "Bash", CallID: "call_1"}},
		{Type: "response.output_item.done", OutputIndex: 0, Item: &ResponsesOutput{Type: "function_call", Name: "Bash", CallID: "call_1"}},
		{Type: "response.function_call_arguments.done", OutputIndex: 0, Arguments: `{"command":"ls"}`},
		{Type: "response.completed", Response: &ResponsesResponse{Status: "completed"}},
	}
	for _, evt := range sequence {
		all = append(all, ResponsesEventToAnthropicEvents(evt, state)...)
	}

	assertAnthropicBlockEventOrder(t, all)
}

// 重复的 function_call_arguments.done 不得在块关闭后再补发孤儿 delta。
func TestResToAnthFuncArgsDone_DuplicateDoneMustNotEmitOrphanDelta(t *testing.T) {
	state := NewResponsesEventToAnthropicState()

	var all []AnthropicStreamEvent
	sequence := []*ResponsesStreamEvent{
		{Type: "response.created", Response: &ResponsesResponse{ID: "resp_1", Model: "grok-4.5"}},
		{Type: "response.output_item.added", OutputIndex: 0, Item: &ResponsesOutput{Type: "function_call", Name: "Bash", CallID: "call_1"}},
		{Type: "response.function_call_arguments.done", OutputIndex: 0, Arguments: `{"command":"ls"}`},
		{Type: "response.function_call_arguments.done", OutputIndex: 0, Arguments: `{"command":"ls"}`},
		{Type: "response.completed", Response: &ResponsesResponse{Status: "completed"}},
	}
	for _, evt := range sequence {
		all = append(all, ResponsesEventToAnthropicEvents(evt, state)...)
	}

	assertAnthropicBlockEventOrder(t, all)
}
