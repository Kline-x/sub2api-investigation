//go:build unit

package apicompat

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// OpenAI Responses API 规范里 response 对象的 created_at 是必填字段。Rust 客户端
// (Codex CLI 等) 用 serde 反序列化时字段缺失会直接报 `missing field created_at`,
// 请求整个失败。本文件锁住所有「非 OpenAI 上游 → Responses 格式」路径都带上该字段。
//
// 注意断言的是**序列化后的 JSON**而不是结构体字段:created_at 一旦带上 omitempty,
// 零值就会被省略,结构体看着有、线上仍然缺,正是这个 bug 的形态。

// requireCreatedAt 断言 JSON 里存在 created_at 且是个合理的秒级时间戳。
func requireCreatedAt(t *testing.T, v any, what string) {
	t.Helper()
	raw, err := json.Marshal(v)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(raw, &decoded))

	value, ok := decoded["created_at"]
	require.Truef(t, ok, "%s: 序列化结果缺少 created_at 字段(Rust 客户端会报 missing field `created_at`);JSON=%s", what, raw)

	ts, ok := value.(float64)
	require.Truef(t, ok, "%s: created_at 应为数字,实际 %T", what, value)
	require.Greaterf(t, int64(ts), time.Now().Add(-time.Hour).Unix(), "%s: created_at 不是合理的秒级时间戳,实际 %v", what, ts)
}

func TestAnthropicToResponsesResponseCarriesCreatedAt(t *testing.T) {
	resp := &AnthropicResponse{
		ID:    "msg_123",
		Model: "claude-sonnet-4",
		Content: []AnthropicContentBlock{
			{Type: "text", Text: "hi"},
		},
	}

	requireCreatedAt(t, AnthropicToResponsesResponse(resp), "AnthropicToResponsesResponse")
}

func TestChatCompletionsResponseToResponsesCarriesCreatedAt(t *testing.T) {
	resp := &ChatCompletionsResponse{
		ID:    "chatcmpl_123",
		Model: "gpt-4o",
		Choices: []ChatChoice{
			{Message: ChatMessage{Role: "assistant", Content: json.RawMessage(`"hi"`)}},
		},
	}

	requireCreatedAt(t, ChatCompletionsResponseToResponses(resp, "gpt-4o", nil, false, nil), "ChatCompletionsResponseToResponses")
}

func TestAnthropicStreamResponsesEventsCarryCreatedAt(t *testing.T) {
	state := NewAnthropicEventToResponsesState()
	state.ResponseID = "resp_123"
	state.Model = "claude-sonnet-4"

	created := makeResponsesCreatedEvent(state)
	requireCreatedAt(t, created.Response, "response.created")

	completed := makeResponsesCompletedEvent(state, "completed", nil)
	requireCreatedAt(t, completed.Response, "response.completed")

	// 同一次响应的 created / completed 必须是同一个时间戳,否则客户端会认为
	// 这是两个不同的 response。
	require.Equal(t, created.Response.CreatedAt, completed.Response.CreatedAt,
		"response.created 与 response.completed 的 created_at 必须一致")
}

// sequence_number 在**每个** Responses SSE 事件里都是必填的,包括流里的第一个事件
// response.created——它的 seq 恒为 0,一旦带 omitempty 就会被整个丢掉,Codex 报
// `missing field sequence_number`。
//
// responses_stream_event_wire.go 的 MarshalJSON 只对带 index 的事件类型显式构造,
// response.created/completed/done/failed/incomplete 走 default 分支的结构体序列化,
// 所以这些类型必须靠 tag 本身(无 omitempty)保证字段存在。
func TestResponsesStreamEventsAlwaysCarrySequenceNumber(t *testing.T) {
	// 覆盖 default 分支的全部事件类型,seq 一律取 0——最容易被 omitempty 吃掉的值。
	for _, eventType := range []string{
		"response.created",
		"response.completed",
		"response.done",
		"response.failed",
		"response.incomplete",
	} {
		t.Run(eventType, func(t *testing.T) {
			raw, err := json.Marshal(ResponsesStreamEvent{
				Type:           eventType,
				SequenceNumber: 0,
				Response:       &ResponsesResponse{ID: "resp_1", Object: "response"},
			})
			require.NoError(t, err)

			var decoded map[string]any
			require.NoError(t, json.Unmarshal(raw, &decoded))
			_, ok := decoded["sequence_number"]
			require.Truef(t, ok, "%s 缺少 sequence_number(Codex 会报 missing field `sequence_number`);JSON=%s", eventType, raw)
		})
	}
}

// 流式转换的第一个事件真实走一遍,确认 seq=0 的 response.created 落到线上仍带字段。
func TestFirstStreamedResponsesEventCarriesSequenceNumber(t *testing.T) {
	state := NewAnthropicEventToResponsesState()
	state.ResponseID = "resp_1"
	state.Model = "claude-sonnet-4"

	first := makeResponsesCreatedEvent(state)
	require.Equal(t, 0, first.SequenceNumber, "第一个事件的 seq 应为 0,正是会被 omitempty 吃掉的值")

	raw, err := json.Marshal(first)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(raw, &decoded))
	require.Containsf(t, decoded, "sequence_number", "response.created 缺少 sequence_number;JSON=%s", raw)

	// 同一个事件里嵌套的 response 对象也必须带 created_at(见上面的用例)。
	nested, ok := decoded["response"].(map[string]any)
	require.True(t, ok)
	require.Containsf(t, nested, "created_at", "response.created 内嵌的 response 缺少 created_at;JSON=%s", raw)
}

// response.completed 携带的 output 数组此前走默认结构体序列化,而 responsesItemWire
// 的显式构造只作用于 response.output_item.added/done 事件。结果是同一个 item 在两处
// 形态不一致:output_item.done 里有 content:[],response.completed 里 content 整个消失
// (function_call 的 arguments:""、reasoning 的 summary:[] 同理)。
// Codex 解析终止事件时同样会报 missing field。
func TestCompletedOutputItemsKeepRequiredZeroValueFields(t *testing.T) {
	items := []ResponsesOutput{
		{Type: "message", ID: "m1", Role: "assistant", Content: nil, Status: "completed"},
		{Type: "function_call", ID: "f1", CallID: "c1", Name: "shell", Arguments: "", Status: "completed"},
		{Type: "reasoning", ID: "r1", Summary: nil, Status: "completed"},
	}

	raw, err := json.Marshal(ResponsesStreamEvent{
		Type:           "response.completed",
		SequenceNumber: 7,
		Response: &ResponsesResponse{
			ID: "resp", Object: "response", Status: "completed", Output: items,
		},
	})
	require.NoError(t, err)

	var decoded struct {
		Response struct {
			Output []map[string]any `json:"output"`
		} `json:"response"`
	}
	require.NoError(t, json.Unmarshal(raw, &decoded))
	require.Len(t, decoded.Response.Output, 3)

	message, functionCall, reasoning := decoded.Response.Output[0], decoded.Response.Output[1], decoded.Response.Output[2]
	require.Containsf(t, message, "content", "message 项缺 content(应为 []);JSON=%s", raw)
	require.Containsf(t, functionCall, "arguments", "function_call 项缺 arguments(空串也必须出现);JSON=%s", raw)
	require.Containsf(t, reasoning, "summary", "reasoning 项缺 summary(应为 []);JSON=%s", raw)
}

// web_search_call 的 action 不在 responsesItemWire 的分支里,统一走 wire 时不能把它丢了。
func TestWebSearchCallOutputKeepsAction(t *testing.T) {
	raw, err := json.Marshal(ResponsesOutput{
		Type:   "web_search_call",
		ID:     "ws1",
		Status: "completed",
		Action: &WebSearchAction{Type: "search", Query: "golang"},
	})
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(raw, &decoded))
	require.Containsf(t, decoded, "action", "web_search_call 丢了 action;JSON=%s", raw)
}

func TestChatCompletionsStreamResponsesEventsCarryCreatedAt(t *testing.T) {
	state := NewChatCompletionsToResponsesStreamState("gpt-4o")

	createdEvents := ensureChatToResponsesCreated(state)
	require.Len(t, createdEvents, 1)
	requireCreatedAt(t, createdEvents[0].Response, "chat response.created")

	completedEvents := FinalizeChatCompletionsResponsesStream(state)
	require.NotEmpty(t, completedEvents)
	terminal := completedEvents[len(completedEvents)-1]
	requireCreatedAt(t, terminal.Response, "chat response.completed")

	require.Equal(t, createdEvents[0].Response.CreatedAt, terminal.Response.CreatedAt,
		"chat response.created 与 response.completed 的 created_at 必须一致")
}

// Anthropic thinking 块在线上必须同时带 thinking 和 signature,两者都是必填。
// 结构体上它们都有 omitempty,空值会被丢掉,严格客户端(grok-shell)报
// `missing field signature`——2026-07-31 实战踩坑,实抓到的 content_block_start
// 是 {"content_block":{"thinking":"","type":"thinking"},...},缺 signature。
func TestAnthropicThinkingBlockAlwaysCarriesThinkingAndSignature(t *testing.T) {
	for _, tc := range []struct {
		name  string
		block AnthropicContentBlock
	}{
		{name: "空值(content_block_start 的形态)", block: AnthropicContentBlock{Type: "thinking"}},
		{name: "有内容无签名", block: AnthropicContentBlock{Type: "thinking", Thinking: "推理中"}},
		{name: "两者都有", block: AnthropicContentBlock{Type: "thinking", Thinking: "推理中", Signature: "sig-abc"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(tc.block)
			require.NoError(t, err)

			var decoded map[string]any
			require.NoError(t, json.Unmarshal(raw, &decoded))
			require.Containsf(t, decoded, "thinking", "thinking 块缺 thinking 字段;JSON=%s", raw)
			require.Containsf(t, decoded, "signature", "thinking 块缺 signature 字段(客户端会报 missing field `signature`);JSON=%s", raw)
			require.Equal(t, tc.block.Signature, decoded["signature"])
			require.Equal(t, tc.block.Thinking, decoded["thinking"])
		})
	}
}

// text 块不应因为上面的改动而多出 signature。
func TestAnthropicTextBlockUnaffectedBySignatureFix(t *testing.T) {
	raw, err := json.Marshal(AnthropicContentBlock{Type: "text", Text: "hi"})
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(raw, &decoded))
	require.Contains(t, decoded, "text")
	require.NotContains(t, decoded, "signature", "text 块不应带 signature")
}
