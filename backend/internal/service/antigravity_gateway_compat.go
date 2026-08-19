package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
)

type antigravityCompatProtocol uint8

const (
	antigravityCompatChatCompletions antigravityCompatProtocol = iota
	antigravityCompatResponses
)

const (
	// AntigravityCredentialRejectedClientMessage 是可安全返回给客户端的认证修复提示。
	AntigravityCredentialRejectedClientMessage = "Antigravity rejected the OAuth credential after refresh; reauthorize the account and verify project_id"
	// AntigravityCredentialRejectedReason 标识上游拒绝已刷新 OAuth 凭据。
	AntigravityCredentialRejectedReason GatewayFailureReason = "antigravity_oauth_credential_rejected"
)

type antigravityCompatRequest struct {
	protocol        antigravityCompatProtocol
	originalBody    []byte
	claudeBody      []byte
	originalModel   string
	clientStream    bool
	includeUsage    bool
	startTime       time.Time
	reasoningEffort *string
	// clientToolMapping 记录 Codex 私有工具（custom / tool_search / namespace）被降级成普通
	// function 工具时的还原信息。仅 Responses 协议会填。详见 adaptAntigravityResponsesClientTools。
	clientToolMapping apicompat.ResponsesClientToolMapping
}

type antigravityCompatUpstreamCall struct {
	request      antigravityCompatRequest
	billingModel string
	prefix       string
	proxyURL     string
	accessToken  string
	geminiBody   []byte
}

// ForwardAsChatCompletions 使用 Antigravity 原生 OAuth 账号转发 Chat Completions 请求。
func (s *AntigravityGatewayService) ForwardAsChatCompletions(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	_ *ParsedRequest,
) (*ForwardResult, error) {
	if err := s.validateAntigravityCompatAccount(c, account); err != nil {
		return nil, err
	}

	// === DEBUG: 客户端原始请求（需 SUB2API_DEBUG_GATEWAY_BODY，未设置时零开销）===
	if c != nil {
		DebugLogGatewaySnapshot("CLIENT_ORIGINAL", c.Request.Header, body, map[string]string{
			"path":         "antigravity/chat_completions",
			"account":      fmt.Sprintf("%d(%s)", account.ID, account.Name),
			"account_type": string(account.Type),
		})
	}

	var request apicompat.ChatCompletionsRequest
	if json.Unmarshal(body, &request) != nil {
		return nil, s.writeAntigravityCompatError(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
	}
	if strings.TrimSpace(request.Model) == "" {
		return nil, s.writeAntigravityCompatError(c, http.StatusBadRequest, "invalid_request_error", "model is required")
	}

	responsesRequest, err := apicompat.ChatCompletionsToResponses(&request)
	if err != nil {
		return nil, s.writeAntigravityCompatError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
	}
	claudeRequest, err := apicompat.ResponsesToAnthropicRequest(responsesRequest)
	if err != nil {
		return nil, s.writeAntigravityCompatError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
	}
	preserveChatCompletionTokenLimit(&request, claudeRequest)
	claudeRequest.Stream = request.Stream
	claudeBody, err := json.Marshal(claudeRequest)
	if err != nil {
		return nil, fmt.Errorf("marshal anthropic request: %w", err)
	}

	return s.forwardAntigravityCompat(ctx, c, account, antigravityCompatRequest{
		protocol:        antigravityCompatChatCompletions,
		originalBody:    body,
		claudeBody:      claudeBody,
		originalModel:   request.Model,
		clientStream:    request.Stream,
		includeUsage:    request.StreamOptions != nil && request.StreamOptions.IncludeUsage,
		startTime:       time.Now(),
		reasoningEffort: extractCCReasoningEffortFromBody(body),
	})
}

// ForwardAsResponses 使用 Antigravity 原生 OAuth 账号转发 Responses 请求。
func (s *AntigravityGatewayService) ForwardAsResponses(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	_ *ParsedRequest,
) (*ForwardResult, error) {
	if err := s.validateAntigravityCompatAccount(c, account); err != nil {
		return nil, err
	}

	// === DEBUG: 客户端原始请求（需 SUB2API_DEBUG_GATEWAY_BODY，未设置时零开销）===
	if c != nil {
		DebugLogGatewaySnapshot("CLIENT_ORIGINAL", c.Request.Header, body, map[string]string{
			"path":         "antigravity/responses",
			"account":      fmt.Sprintf("%d(%s)", account.ID, account.Name),
			"account_type": string(account.Type),
		})
	}

	// Codex 私有工具降级：tool_search / namespace / custom → 普通 function 工具。
	// 必须在解析成 ResponsesRequest 之前做，因为降级会改写 tools 与 input 里的历史项。
	adaptedBody, clientToolMapping, err := adaptAntigravityResponsesClientTools(body)
	if err != nil {
		return nil, s.writeAntigravityCompatError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
	}

	var request apicompat.ResponsesRequest
	if json.Unmarshal(adaptedBody, &request) != nil {
		return nil, s.writeAntigravityCompatError(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
	}
	if strings.TrimSpace(request.Model) == "" {
		return nil, s.writeAntigravityCompatError(c, http.StatusBadRequest, "invalid_request_error", "model is required")
	}

	claudeRequest, err := apicompat.ResponsesToAnthropicRequest(&request)
	if err != nil {
		return nil, s.writeAntigravityCompatError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
	}
	claudeRequest.Stream = request.Stream
	claudeBody, err := json.Marshal(claudeRequest)
	if err != nil {
		return nil, fmt.Errorf("marshal anthropic request: %w", err)
	}

	return s.forwardAntigravityCompat(ctx, c, account, antigravityCompatRequest{
		protocol:          antigravityCompatResponses,
		originalBody:      body,
		claudeBody:        claudeBody,
		originalModel:     request.Model,
		clientStream:      request.Stream,
		startTime:         time.Now(),
		reasoningEffort:   ExtractResponsesReasoningEffortFromBody(body),
		clientToolMapping: clientToolMapping,
	})
}

// adaptAntigravityResponsesClientTools 把 Codex 的客户端专有工具降级成上游能理解的 function 工具。
//
// 【为什么】Codex 的 Responses 请求里有三类非标工具：
//   - {"type":"tool_search"}        —— 无 name，用于按需加载延迟工具（子 agent 的 multi_agent_v1
//     就只能通过它拿到）
//   - {"type":"namespace", ...}     —— 工具集，真正可调的是它 tools[] 里的子工具
//   - {"type":"custom", ...}        —— freeform 工具，入参是自由文本而非 JSON
//
// 而 Antigravity 链路是 Responses → Anthropic → Gemini 两跳转换，
// convertResponsesToAnthropicTools 只认 function/custom/web_search，其余走 default 原样透传：
// tool_search 变成一个无 name 的工具，被 antigravity 的 buildTools 以
// "skipping tool with empty name" 丢弃；namespace 变成一个空参数的假函数，子工具全丢。
//
// 结果是模型既搜不到延迟工具、又看到一堆调了没用的壳工具，只能瞎猜着直接调 namespace 名，
// Codex 收到无法路由的调用后回 "unsupported call: multi_agent_v1"（2026-08-19 从 Codex
// rollout 日志确认：正常链路是 tool_search_call → tool_search_output(namespace multi_agent_v1)
// → 调 spawn_agent，而 Antigravity 链路上第一步就不存在）。
//
// 复用 apicompat.AdaptResponsesClientTools —— 上游已经为 OpenAI / Grok 路径写好了这套降级与
// 还原，只是一直没接到 Antigravity 上。additional_tools 的摊平同样沿用 Responses 直转路径的做法。
func adaptAntigravityResponsesClientTools(body []byte) ([]byte, apicompat.ResponsesClientToolMapping, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var requestBody map[string]any
	if err := decoder.Decode(&requestBody); err != nil {
		return body, apicompat.ResponsesClientToolMapping{}, err
	}
	additionalToolsChanged, err := liftResponsesAdditionalTools(requestBody)
	if err != nil {
		return body, apicompat.ResponsesClientToolMapping{}, err
	}
	mapping, changed, err := apicompat.AdaptResponsesClientTools(requestBody)
	if err != nil {
		return body, apicompat.ResponsesClientToolMapping{}, err
	}
	if !changed && !additionalToolsChanged {
		return body, mapping, nil
	}
	rebuilt, err := json.Marshal(requestBody)
	if err != nil {
		return body, apicompat.ResponsesClientToolMapping{}, err
	}
	return rebuilt, mapping, nil
}

func (s *AntigravityGatewayService) validateAntigravityCompatAccount(c *gin.Context, account *Account) error {
	if account != nil && account.Platform == PlatformAntigravity && account.Type == AccountTypeOAuth {
		return nil
	}
	return s.writeAntigravityCompatError(
		c,
		http.StatusBadRequest,
		"invalid_request_error",
		"native OAuth account required for antigravity compatibility mode",
	)
}

func preserveChatCompletionTokenLimit(request *apicompat.ChatCompletionsRequest, claudeRequest *apicompat.AnthropicRequest) {
	if request == nil || claudeRequest == nil {
		return
	}
	limit := request.MaxTokens
	if request.MaxCompletionTokens != nil {
		limit = request.MaxCompletionTokens
	}
	if limit != nil && *limit > 0 {
		claudeRequest.MaxTokens = *limit
	}
}

func (s *AntigravityGatewayService) forwardAntigravityCompat(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	request antigravityCompatRequest,
) (*ForwardResult, error) {
	beginUpstreamResponseModelObservation(c)
	call, err := s.prepareAntigravityCompatCall(ctx, c, account, request)
	if err != nil {
		return nil, err
	}

	result, err := s.antigravityRetryLoop(antigravityRetryLoopParams{
		ctx:             ctx,
		prefix:          call.prefix,
		account:         account,
		proxyURL:        call.proxyURL,
		accessToken:     call.accessToken,
		action:          "streamGenerateContent",
		body:            call.geminiBody,
		c:               c,
		httpUpstream:    s.httpUpstream,
		settingService:  s.settingService,
		accountRepo:     s.accountRepo,
		handleError:     s.handleUpstreamError,
		requestedModel:  request.originalModel,
		isStickySession: false,
		groupID:         0,
		sessionHash:     "",
	})
	if err != nil {
		return nil, s.handleAntigravityCompatTransportError(c, err)
	}

	return s.consumeAntigravityCompatResponse(ctx, c, account, call, result.resp)
}

func (s *AntigravityGatewayService) prepareAntigravityCompatCall(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	request antigravityCompatRequest,
) (*antigravityCompatUpstreamCall, error) {
	var claudeRequest antigravity.ClaudeRequest
	if json.Unmarshal(request.claudeBody, &claudeRequest) != nil {
		return nil, s.writeAntigravityCompatError(c, http.StatusBadRequest, "invalid_request_error", "Invalid request body")
	}

	mappedModel := s.getMappedModel(account, request.originalModel)
	if mappedModel == "" {
		MarkOpsClientBusinessLimited(c, OpsClientBusinessLimitedReasonLocalFeatureGate)
		message := fmt.Sprintf("model %s not in whitelist", request.originalModel)
		return nil, s.writeAntigravityCompatError(c, http.StatusForbidden, "permission_error", message)
	}
	thinkingEnabled := claudeRequest.Thinking != nil &&
		(claudeRequest.Thinking.Type == "enabled" || claudeRequest.Thinking.Type == "adaptive")
	mappedModel = applyThinkingModelSuffix(mappedModel, thinkingEnabled)

	if s.tokenProvider == nil {
		return nil, s.writeAntigravityCompatError(c, http.StatusBadGateway, "api_error", "Antigravity token provider not configured")
	}
	accessToken, err := s.tokenProvider.GetAccessToken(ctx, account)
	if err != nil {
		return nil, &UpstreamFailoverError{
			StatusCode:   http.StatusBadGateway,
			ResponseBody: []byte(`{"error":{"type":"authentication_error","message":"Failed to get upstream access token"},"type":"error"}`),
		}
	}

	projectID, err := resolveAntigravityProjectID(account)
	if err != nil {
		_ = s.writeAntigravityCompatError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return nil, err
	}
	geminiBody, err := s.buildAntigravityCompatGeminiBody(ctx, request.claudeBody, &claudeRequest, projectID, mappedModel)
	if err != nil {
		return nil, s.writeAntigravityCompatError(c, http.StatusBadRequest, "invalid_request_error", "Invalid request")
	}

	// === DEBUG: 中间态（转成 Anthropic 之后）与最终转发给上游的 Gemini body ===
	DebugLogGatewaySnapshot("ANTHROPIC_INTERMEDIATE", nil, request.claudeBody, map[string]string{
		"path":         "antigravity/compat",
		"account":      fmt.Sprintf("%d(%s)", account.ID, account.Name),
		"mapped_model": mappedModel,
	})
	DebugLogGatewaySnapshot("UPSTREAM_FORWARD", nil, geminiBody, map[string]string{
		"path":         "antigravity/compat",
		"account":      fmt.Sprintf("%d(%s)", account.ID, account.Name),
		"mapped_model": mappedModel,
		"transformer":  "buildAntigravityCompatGeminiBody",
	})

	request.reasoningEffort = ApplyThinkingEnabledFallback(request.reasoningEffort, request.originalBody, mappedModel)
	return &antigravityCompatUpstreamCall{
		request:      request,
		billingModel: mappedModel,
		prefix:       logPrefix(getSessionID(c), account.Name),
		proxyURL:     antigravityCompatProxyURL(account),
		accessToken:  accessToken,
		geminiBody:   geminiBody,
	}, nil
}

// dropAntigravityBuiltinToolsWhenFunctionsPresent 当请求里同时存在内置工具
// （server-side tool，目前是 googleSearch）与函数工具（functionDeclarations）时，
// 丢掉内置工具，只保留函数工具。
//
// 【为什么】Antigravity 的 v1internal 端点不支持两者共存，混用一律 400 INVALID_ARGUMENT：
//
//	Please enable tool_config.include_server_side_tool_invocations
//	to use Built-in tools with Function calling.
//
// 而那个开关在该端点上**不生效**——2026-08-17 实测：字段在 schema 里（TYPE_BOOL）、
// 值确实送达上游，但 camel/snake 两种拼写、把两类工具合并进单个 tool 对象、
// functionCallingConfig 的 AUTO/ANY/NONE/VALIDATED 四种 mode，全部照旧 400，
// gemini-3.6 与 3.7 表现一致。上游转换器在检测到 web_search 时会把 requestType 切成
// "web_search" 并降级模型，也印证了「内置搜索是独立请求类型，不与函数调用同场」。
//
// 【取舍】丢内置搜索、保函数工具：函数工具是 Codex / CC Switch 这类客户端的命脉
// （shell / apply_patch 等），丢了整个会话就废；服务端搜索只是锦上添花。
//
// 【范围】只作用于 Antigravity 路径。convertClaudeMessagesToGeminiGenerateContent 是
// **Gemini 平台与 Antigravity 共用**的，而 Gemini 平台上混用完全正常——
// 2026-08-17 用真实 Gemini 账号（google_one OAuth）实测：同样的
// web_search + 函数工具组合走 Gemini 平台返回 200，连这个开关都不需要带。
// 所以丢弃逻辑必须放在 Antigravity 专属链路里，塞进共用转换器会把 Gemini 平台上
// 本来可用的混用能力一起砍掉。差异出在**端点**（cloudcode-pa 内部端点 vs 公开 Gemini API），
// 不是模型。
func dropAntigravityBuiltinToolsWhenFunctionsPresent(body []byte) []byte {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return body
	}

	tools, ok := payload["tools"].([]any)
	if !ok || len(tools) == 0 {
		return body
	}

	hasFunctions := false
	for _, raw := range tools {
		tool, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if decls, exists := tool["functionDeclarations"].([]any); exists && len(decls) > 0 {
			hasFunctions = true
			break
		}
		if decls, exists := tool["function_declarations"].([]any); exists && len(decls) > 0 {
			hasFunctions = true
			break
		}
	}
	if !hasFunctions {
		return body
	}

	kept := make([]any, 0, len(tools))
	dropped := 0
	for _, raw := range tools {
		tool, ok := raw.(map[string]any)
		if !ok {
			kept = append(kept, raw)
			continue
		}
		_, hasGoogleSearch := tool["googleSearch"]
		if !hasGoogleSearch {
			_, hasGoogleSearch = tool["google_search"]
		}
		if !hasGoogleSearch {
			kept = append(kept, raw)
			continue
		}
		// 同一个 tool 对象里既有内置搜索又有函数声明时，只摘掉内置搜索的键
		delete(tool, "googleSearch")
		delete(tool, "google_search")
		dropped++
		if len(tool) > 0 {
			kept = append(kept, tool)
		}
	}
	if dropped == 0 {
		return body
	}

	logger.LegacyPrintf("service.antigravity_gateway",
		"[antigravity-compat] 内置 web_search 与函数工具混用，上游不支持，已丢弃 %d 个内置搜索工具", dropped)

	if len(kept) == 0 {
		delete(payload, "tools")
	} else {
		payload["tools"] = kept
	}

	out, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return out
}

func (s *AntigravityGatewayService) buildAntigravityCompatGeminiBody(
	ctx context.Context,
	claudeBody []byte,
	claudeRequest *antigravity.ClaudeRequest,
	projectID string,
	mappedModel string,
) ([]byte, error) {
	if strings.HasPrefix(strings.ToLower(mappedModel), "gemini-") {
		body, err := convertClaudeMessagesToGeminiGenerateContent(claudeBody)
		if err != nil {
			return nil, err
		}
		body = dropAntigravityBuiltinToolsWhenFunctionsPresent(body)
		body = ensureGeminiFunctionCallThoughtSignatures(body)
		body, err = injectIdentityPatchToGeminiRequest(body)
		if err != nil {
			return nil, err
		}
		if cleaned, cleanErr := cleanGeminiRequest(body); cleanErr == nil {
			body = cleaned
		}
		return s.wrapV1InternalRequest(projectID, mappedModel, body)
	}

	options := s.getClaudeTransformOptions(ctx)
	options.EnableIdentityPatch = true
	return antigravity.TransformClaudeToGeminiWithOptions(claudeRequest, projectID, mappedModel, options)
}

func antigravityCompatProxyURL(account *Account) string {
	if account.ProxyID == nil || account.Proxy == nil {
		return ""
	}
	return account.Proxy.URL()
}

func (s *AntigravityGatewayService) handleAntigravityCompatTransportError(c *gin.Context, err error) error {
	if switchErr, ok := IsAntigravityAccountSwitchError(err); ok {
		return &UpstreamFailoverError{
			StatusCode:        http.StatusServiceUnavailable,
			ForceCacheBilling: switchErr.IsStickySession,
		}
	}
	if c.Request.Context().Err() != nil {
		return s.writeAntigravityCompatError(c, http.StatusBadGateway, "client_disconnected", "Client disconnected before upstream response")
	}
	return s.writeAntigravityCompatError(c, http.StatusBadGateway, "upstream_error", "Upstream request failed after retries")
}

func (s *AntigravityGatewayService) consumeAntigravityCompatResponse(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	call *antigravityCompatUpstreamCall,
	resp *http.Response,
) (*ForwardResult, error) {
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, s.handleAntigravityCompatHTTPError(ctx, c, account, call, resp)
	}

	requestID := resp.Header.Get("x-request-id")
	if requestID != "" {
		c.Header("x-request-id", requestID)
	}
	streamResult, err := s.consumeAntigravityCompatSuccess(c, call, resp)
	if err != nil {
		return nil, err
	}
	if streamResult.usage == nil {
		streamResult.usage = &ClaudeUsage{}
	}

	return &ForwardResult{
		RequestID:                     requestID,
		Usage:                         *streamResult.usage,
		Model:                         call.request.originalModel,
		UpstreamModel:                 call.billingModel,
		UpstreamResponseModel:         observedUpstreamResponseModel(c),
		UpstreamResponseModelConflict: observedUpstreamResponseModelConflict(c),
		Stream:                        call.request.clientStream,
		Duration:                      time.Since(call.request.startTime),
		FirstTokenMs:                  streamResult.firstTokenMs,
		ReasoningEffort:               call.request.reasoningEffort,
		ClientDisconnect:              streamResult.clientDisconnect,
	}, nil
}

func (s *AntigravityGatewayService) consumeAntigravityCompatSuccess(
	c *gin.Context,
	call *antigravityCompatUpstreamCall,
	resp *http.Response,
) (*antigravityStreamResult, error) {
	if call.request.clientStream {
		if call.request.protocol == antigravityCompatChatCompletions {
			return s.handleChatCompletionsStreamingFromAntigravity(
				c,
				resp,
				call.request.startTime,
				call.request.originalModel,
				call.request.includeUsage,
			)
		}
		return s.handleResponsesStreamingFromAntigravity(c, resp, call.request.startTime, call.request.originalModel, call.request.clientToolMapping)
	}

	if call.request.protocol == antigravityCompatChatCompletions {
		return s.handleChatCompletionsNonStreamingFromAntigravity(c, resp, call.request.startTime, call.request.originalModel)
	}
	return s.handleResponsesNonStreamingFromAntigravity(c, resp, call.request.startTime, call.request.originalModel, call.request.clientToolMapping)
}

func (s *AntigravityGatewayService) handleAntigravityCompatHTTPError(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	call *antigravityCompatUpstreamCall,
	resp *http.Response,
) error {
	body := s.readUpstreamErrorBody(resp)
	s.handleUpstreamError(
		ctx,
		call.prefix,
		account,
		resp.StatusCode,
		resp.Header,
		body,
		call.request.originalModel,
		0,
		"",
		false,
	)
	if s.shouldFailoverUpstreamError(resp.StatusCode) {
		message := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractAntigravityErrorMessage(body)))
		event := OpsUpstreamErrorEvent{
			Platform:           account.Platform,
			AccountID:          account.ID,
			AccountName:        account.Name,
			UpstreamStatusCode: resp.StatusCode,
			UpstreamRequestID:  resp.Header.Get("x-request-id"),
			Kind:               "failover",
			Message:            message,
			Detail:             s.getUpstreamErrorDetail(body),
		}
		if resp.StatusCode == http.StatusUnauthorized {
			event.Stage = string(GatewayFailureStageAccountAuth)
			event.Scope = string(GatewayFailureScopeAccount)
			event.Reason = string(AntigravityCredentialRejectedReason)
			appendOpsUpstreamError(c, event)
			return antigravityCredentialRejectedError(resp, body)
		}
		appendOpsUpstreamError(c, event)
		return &UpstreamFailoverError{
			StatusCode:      resp.StatusCode,
			ResponseBody:    body,
			ResponseHeaders: resp.Header.Clone(),
		}
	}
	return s.writeMappedAntigravityCompatError(c, account, resp.StatusCode, resp.Header.Get("x-request-id"), body)
}

func antigravityCredentialRejectedError(resp *http.Response, body []byte) *UpstreamFailoverError {
	return &UpstreamFailoverError{
		StatusCode:        resp.StatusCode,
		ResponseBody:      body,
		ResponseHeaders:   resp.Header.Clone(),
		Stage:             GatewayFailureStageAccountAuth,
		Scope:             GatewayFailureScopeAccount,
		Reason:            AntigravityCredentialRejectedReason,
		NextAccountAction: NextAccountRetry,
		ClientStatusCode:  http.StatusBadGateway,
		ClientMessage:     AntigravityCredentialRejectedClientMessage,
	}
}

func (s *AntigravityGatewayService) writeAntigravityCompatError(
	c *gin.Context,
	status int,
	errType string,
	message string,
) error {
	MarkResponseCommitted(c)
	c.JSON(status, gin.H{
		"error": gin.H{
			"message": message,
			"type":    errType,
			"param":   nil,
			"code":    nil,
		},
	})
	return errors.New(message)
}

func (s *AntigravityGatewayService) writeMappedAntigravityCompatError(
	c *gin.Context,
	account *Account,
	upstreamStatus int,
	upstreamRequestID string,
	body []byte,
) error {
	MarkResponseCommitted(c)
	message := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractAntigravityErrorMessage(body)))
	setOpsUpstreamError(c, upstreamStatus, message, s.getUpstreamErrorDetail(body))
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
		Platform:           account.Platform,
		AccountID:          account.ID,
		AccountName:        account.Name,
		UpstreamStatusCode: upstreamStatus,
		UpstreamRequestID:  upstreamRequestID,
		Kind:               "http_error",
		Message:            message,
	})
	c.JSON(mapUpstreamStatusCode(upstreamStatus), gin.H{
		"error": gin.H{
			"message": getPassthroughOrDefault(message, "Upstream request failed"),
			"type":    "upstream_error",
			"param":   nil,
			"code":    nil,
		},
	})
	return fmt.Errorf("upstream error: %d %s", upstreamStatus, message)
}

func (s *AntigravityGatewayService) handleChatCompletionsNonStreamingFromAntigravity(
	c *gin.Context,
	resp *http.Response,
	startTime time.Time,
	originalModel string,
) (*antigravityStreamResult, error) {
	claudeResponse, result, err := s.collectClaudeStreamResponse(c, resp, startTime, originalModel)
	if err != nil {
		return nil, s.mapAntigravityCompatCollectionError(c, err)
	}
	var anthropicResponse apicompat.AnthropicResponse
	if json.Unmarshal(claudeResponse, &anthropicResponse) != nil {
		return nil, s.writeAntigravityCompatError(c, http.StatusBadGateway, "upstream_error", "Failed to parse upstream response")
	}
	responsesResponse := apicompat.AnthropicToResponsesResponse(&anthropicResponse)
	c.JSON(http.StatusOK, apicompat.ResponsesToChatCompletions(responsesResponse, originalModel))
	return result, nil
}

// handleResponsesNonStreamingFromAntigravity 处理 Responses 协议的非流式响应。
// mapping 可选：语义同流式版本。
func (s *AntigravityGatewayService) handleResponsesNonStreamingFromAntigravity(
	c *gin.Context,
	resp *http.Response,
	startTime time.Time,
	originalModel string,
	mapping ...apicompat.ResponsesClientToolMapping,
) (*antigravityStreamResult, error) {
	claudeResponse, result, err := s.collectClaudeStreamResponse(c, resp, startTime, originalModel)
	if err != nil {
		return nil, s.mapAntigravityCompatCollectionError(c, err)
	}
	var anthropicResponse apicompat.AnthropicResponse
	if json.Unmarshal(claudeResponse, &anthropicResponse) != nil {
		return nil, s.writeAntigravityCompatError(c, http.StatusBadGateway, "upstream_error", "Failed to parse upstream response")
	}

	responsesResponse := apicompat.AnthropicToResponsesResponse(&anthropicResponse)
	if len(mapping) > 0 && (len(mapping[0].CustomTools) > 0 || mapping[0].ToolSearch || len(mapping[0].NamespaceTools) > 0) {
		if payload, marshalErr := json.Marshal(responsesResponse); marshalErr == nil {
			if restored, _, restoreErr := apicompat.RestoreResponsesClientToolPayload(payload, mapping[0]); restoreErr == nil {
				c.Data(http.StatusOK, "application/json; charset=utf-8", restored)
				return result, nil
			}
		}
		// 还原失败时退回未还原的响应，保证请求不至于失败。
	}
	c.JSON(http.StatusOK, responsesResponse)
	return result, nil
}

func (s *AntigravityGatewayService) mapAntigravityCompatCollectionError(c *gin.Context, err error) error {
	var failoverError *UpstreamFailoverError
	if errors.As(err, &failoverError) {
		return err
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if strings.Contains(err.Error(), "stream data interval timeout") {
		return s.writeAntigravityCompatError(c, http.StatusBadGateway, "upstream_timeout", "Upstream stream data interval timeout")
	}
	if errors.Is(err, bufio.ErrTooLong) {
		return s.writeAntigravityCompatError(c, http.StatusBadGateway, "response_too_large", "Upstream response line too long")
	}
	return s.writeAntigravityCompatError(c, http.StatusBadGateway, "upstream_error", "Failed to parse upstream response")
}
