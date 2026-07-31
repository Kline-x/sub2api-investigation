//go:build unit

package apicompat

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// 严格客户端(Codex CLI 等 Rust/serde 实现)对 Responses SSE 的每个事件与嵌套对象
// 都有必填字段要求,字段缺失即 `missing field xxx` 让整条流失败。Go 的 omitempty
// 恰好会丢掉这些「零值但有意义」的字段(空数组、空字符串、索引 0)。
//
// 这个文件不针对单个字段,而是把一条**完整的流**跑出来,对每个事件按类型逐条核对
// 必填字段——目的是一次性暴露所有缺口,不再靠线上报错一个个补。

// requiredByEventType 是各事件类型在线上必须出现的顶层字段。
var requiredByEventType = map[string][]string{
	"response.created":                       {"type", "sequence_number", "response"},
	"response.in_progress":                   {"type", "sequence_number", "response"},
	"response.completed":                     {"type", "sequence_number", "response"},
	"response.incomplete":                    {"type", "sequence_number", "response"},
	"response.failed":                        {"type", "sequence_number", "response"},
	"response.output_item.added":             {"type", "sequence_number", "output_index", "item"},
	"response.output_item.done":              {"type", "sequence_number", "output_index", "item"},
	"response.content_part.added":            {"type", "sequence_number", "output_index", "content_index", "part"},
	"response.content_part.done":             {"type", "sequence_number", "output_index", "content_index", "part"},
	"response.output_text.delta":             {"type", "sequence_number", "output_index", "content_index", "delta"},
	"response.output_text.done":              {"type", "sequence_number", "output_index", "content_index", "text"},
	"response.reasoning_summary_part.added":  {"type", "sequence_number", "output_index", "summary_index", "part"},
	"response.reasoning_summary_part.done":   {"type", "sequence_number", "output_index", "summary_index", "part"},
	"response.reasoning_summary_text.delta":  {"type", "sequence_number", "output_index", "summary_index", "delta"},
	"response.reasoning_summary_text.done":   {"type", "sequence_number", "output_index", "summary_index", "text"},
	"response.function_call_arguments.delta": {"type", "sequence_number", "output_index", "delta"},
	"response.function_call_arguments.done":  {"type", "sequence_number", "output_index", "arguments"},
	"response.custom_tool_call_input.delta":  {"type", "sequence_number", "output_index", "delta"},
	"response.custom_tool_call_input.done":   {"type", "sequence_number", "output_index", "input"},
}

// requiredByItemType 是 output item 按类型必须出现的字段。
var requiredByItemType = map[string][]string{
	"message":          {"type", "id", "role", "content"},
	"reasoning":        {"type", "id", "summary"},
	"function_call":    {"type", "id", "call_id", "name", "arguments"},
	"custom_tool_call": {"type", "id", "call_id", "name", "input"},
}

// requiredUsageFields / requiredUsageDetailFields 是 usage 及其明细必须出现的字段。
// Codex 对 usage.input_tokens_details.cached_tokens 有硬要求,缺失即
// `missing field input_tokens_details`。
var (
	requiredUsageFields              = []string{"input_tokens", "output_tokens", "total_tokens", "input_tokens_details", "output_tokens_details"}
	requiredInputTokensDetailFields  = []string{"cached_tokens"}
	requiredOutputTokensDetailFields = []string{"reasoning_tokens"}
)

// requiredByPartType 是 content part / summary part 按类型必须出现的字段。
var requiredByPartType = map[string][]string{
	"output_text":  {"type", "text", "annotations"},
	"summary_text": {"type", "text"},
}

func checkRequired(t *testing.T, obj map[string]any, required []string, what string, raw []byte) {
	t.Helper()
	for _, field := range required {
		require.Containsf(t, obj, field, "%s 缺必填字段 %q(严格客户端会报 missing field `%s`);JSON=%s", what, field, field, raw)
	}
}

// verifyEvent 递归核对一个事件及其嵌套的 item / part / output 数组。
func verifyEvent(t *testing.T, evt ResponsesStreamEvent) {
	t.Helper()
	raw, err := json.Marshal(evt)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(raw, &decoded))

	if required, ok := requiredByEventType[evt.Type]; ok {
		checkRequired(t, decoded, required, "事件 "+evt.Type, raw)
	}

	if item, ok := decoded["item"].(map[string]any); ok {
		verifyItem(t, item, "事件 "+evt.Type+" 的 item", raw)
	}
	if part, ok := decoded["part"].(map[string]any); ok {
		verifyPart(t, part, "事件 "+evt.Type+" 的 part", raw)
	}
	if response, ok := decoded["response"].(map[string]any); ok {
		checkRequired(t, response, []string{"id", "object", "created_at", "model", "status", "output"}, "事件 "+evt.Type+" 的 response", raw)
		if outputs, ok := response["output"].([]any); ok {
			for i, o := range outputs {
				if item, ok := o.(map[string]any); ok {
					verifyItem(t, item, fmt.Sprintf("事件 %s 的 response.output[%d]", evt.Type, i), raw)
				}
			}
		}
		if usage, ok := response["usage"].(map[string]any); ok {
			verifyUsage(t, usage, "事件 "+evt.Type+" 的 response.usage", raw)
		}
	}
	if usage, ok := decoded["usage"].(map[string]any); ok {
		verifyUsage(t, usage, "事件 "+evt.Type+" 的顶层 usage", raw)
	}
}

func verifyUsage(t *testing.T, usage map[string]any, what string, raw []byte) {
	t.Helper()
	checkRequired(t, usage, requiredUsageFields, what, raw)
	if details, ok := usage["input_tokens_details"].(map[string]any); ok {
		checkRequired(t, details, requiredInputTokensDetailFields, what+".input_tokens_details", raw)
	}
	if details, ok := usage["output_tokens_details"].(map[string]any); ok {
		checkRequired(t, details, requiredOutputTokensDetailFields, what+".output_tokens_details", raw)
	}
}

func verifyItem(t *testing.T, item map[string]any, what string, raw []byte) {
	t.Helper()
	itemType, _ := item["type"].(string)
	if required, ok := requiredByItemType[itemType]; ok {
		checkRequired(t, item, required, what+"("+itemType+")", raw)
	}
	if contents, ok := item["content"].([]any); ok {
		for i, c := range contents {
			if part, ok := c.(map[string]any); ok {
				verifyPart(t, part, fmt.Sprintf("%s 的 content[%d]", what, i), raw)
			}
		}
	}
	if summaries, ok := item["summary"].([]any); ok {
		for i, sm := range summaries {
			if part, ok := sm.(map[string]any); ok {
				verifyPart(t, part, fmt.Sprintf("%s 的 summary[%d]", what, i), raw)
			}
		}
	}
}

func verifyPart(t *testing.T, part map[string]any, what string, raw []byte) {
	t.Helper()
	partType, _ := part["type"].(string)
	if required, ok := requiredByPartType[partType]; ok {
		checkRequired(t, part, required, what+"("+partType+")", raw)
	}
}

// 把一条包含 thinking + text + tool_use 的完整 Anthropic 流转成 Responses 事件,
// 逐个事件核对必填字段。覆盖 reasoning / message / function_call 三条 item 路径。
func TestAnthropicStreamToResponsesEventsAllCarryRequiredFields(t *testing.T) {
	state := NewAnthropicEventToResponsesState()
	state.ResponseID = "resp_full"
	state.Model = "claude-sonnet-4"

	idx := func(i int) *int { return &i }
	upstream := []AnthropicStreamEvent{
		{Type: "message_start", Message: &AnthropicResponse{ID: "msg_1", Model: "claude-sonnet-4", Usage: AnthropicUsage{InputTokens: 10}}},

		// reasoning
		{Type: "content_block_start", Index: idx(0), ContentBlock: &AnthropicContentBlock{Type: "thinking"}},
		{Type: "content_block_delta", Index: idx(0), Delta: &AnthropicDelta{Type: "thinking_delta", Thinking: "think"}},
		{Type: "content_block_stop", Index: idx(0)},

		// message text
		{Type: "content_block_start", Index: idx(1), ContentBlock: &AnthropicContentBlock{Type: "text"}},
		{Type: "content_block_delta", Index: idx(1), Delta: &AnthropicDelta{Type: "text_delta", Text: "hello"}},
		{Type: "content_block_stop", Index: idx(1)},

		// tool call
		{Type: "content_block_start", Index: idx(2), ContentBlock: &AnthropicContentBlock{Type: "tool_use", ID: "toolu_1", Name: "shell"}},
		{Type: "content_block_delta", Index: idx(2), Delta: &AnthropicDelta{Type: "input_json_delta", PartialJSON: `{"cmd":"ls"}`}},
		{Type: "content_block_stop", Index: idx(2)},

		{Type: "message_delta", Usage: &AnthropicUsage{OutputTokens: 5}},
		{Type: "message_stop"},
	}

	var seen []string
	for _, up := range upstream {
		evt := up
		for _, out := range AnthropicEventToResponsesEvents(&evt, state) {
			seen = append(seen, out.Type)
			verifyEvent(t, out)
		}
	}

	// 确认这条流确实覆盖到了关键事件,否则断言等于没跑
	require.Contains(t, seen, "response.created")
	require.Contains(t, seen, "response.output_item.added")
	require.Contains(t, seen, "response.output_text.delta")
	require.Contains(t, seen, "response.completed")
	t.Logf("覆盖事件类型: %v", seen)
}

// 非流式响应里的 output item / content part 走的是另一条序列化路径,同样核对。
func TestAnthropicNonStreamResponseCarriesRequiredFields(t *testing.T) {
	resp := &AnthropicResponse{
		ID:    "msg_2",
		Model: "claude-sonnet-4",
		Content: []AnthropicContentBlock{
			{Type: "thinking", Thinking: "think"},
			{Type: "text", Text: "hello"},
			{Type: "tool_use", ID: "toolu_2", Name: "shell", Input: json.RawMessage(`{"cmd":"ls"}`)},
		},
	}

	converted := AnthropicToResponsesResponse(resp)
	raw, err := json.Marshal(converted)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(raw, &decoded))
	checkRequired(t, decoded, []string{"id", "object", "created_at", "model", "status", "output"}, "非流式 response", raw)

	outputs, ok := decoded["output"].([]any)
	require.True(t, ok)
	for i, o := range outputs {
		item, ok := o.(map[string]any)
		require.True(t, ok)
		verifyItem(t, item, fmt.Sprintf("非流式 output[%d]", i), raw)
	}

	usage, ok := decoded["usage"].(map[string]any)
	require.Truef(t, ok, "非流式 response 缺 usage;JSON=%s", raw)
	verifyUsage(t, usage, "非流式 response.usage", raw)
}

// ChatCompletions→Responses 是另一条独立的转换链路(OpenAI 兼容上游走它),
// 同样做全事件必填字段核对,避免只修了 Anthropic 那条。
func TestChatCompletionsStreamToResponsesEventsAllCarryRequiredFields(t *testing.T) {
	state := NewChatCompletionsToResponsesStreamState("gpt-4o")

	var seen []string
	emit := func(events []ResponsesStreamEvent) {
		for _, evt := range events {
			seen = append(seen, evt.Type)
			verifyEvent(t, evt)
		}
	}

	emit(ensureChatToResponsesCreated(state))

	text := "hello"
	stop := "stop"
	toolIndex := 0
	chunks := []ChatCompletionsChunk{
		{Choices: []ChatChunkChoice{{Delta: ChatDelta{Content: &text}}}},
		{Choices: []ChatChunkChoice{{Delta: ChatDelta{
			ToolCalls: []ChatToolCall{{
				Index:    &toolIndex,
				ID:       "call_1",
				Type:     "function",
				Function: ChatFunctionCall{Name: "shell", Arguments: `{"cmd":"ls"}`},
			}},
		}}}},
		{Choices: []ChatChunkChoice{{FinishReason: &stop}}},
	}
	for i := range chunks {
		emit(ChatCompletionsChunkToResponsesEvents(&chunks[i], state))
	}
	emit(FinalizeChatCompletionsResponsesStream(state))

	require.Contains(t, seen, "response.created")
	require.Contains(t, seen, "response.completed")
	t.Logf("覆盖事件类型: %v", seen)
}
