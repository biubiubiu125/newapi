package convmeta

import (
	"strings"

	"github.com/QuantumNous/new-api/relaykit/types"
)

// Options is the per-request snapshot of host configuration that converters
// consult. The host fills it from its settings system when constructing the
// Meta (see relaycommon.RelayInfo.ConvOptions); relaykit users fill it
// directly. Zero value = every adaptation disabled, no defaults applied.
type Options struct {
	Claude      ClaudeOptions
	Gemini      GeminiOptions
	HostedTools HostedToolCapabilities

	// ToolLossPolicy controls whether a cross-protocol conversion may omit or
	// approximate built-in-tool semantics. The zero value uses the allow
	// policy: conversion succeeds and every loss is returned as a diagnostic.
	// safe/strict rejection is request-phase opt-in only; response and stream
	// conversion never reject regardless of this field.
	ToolLossPolicy types.ConversionLossPolicy

	// OpenRouterDialect marks the upstream as OpenRouter's OpenAI-compatible
	// surface, which accepts extra fields (reasoning config, cache_control on
	// system parts) that converters emit only for that dialect. The host sets
	// it from the channel type.
	OpenRouterDialect bool

	// PreserveThinkingSuffix reports models whose -thinking/-nothinking/effort
	// suffix must be kept on the outgoing model name (host blacklist lookup).
	// Nil means "never preserve".
	PreserveThinkingSuffix func(modelName string) bool
	// PreserveEffortTail reports real model IDs whose names already end in an
	// effort-like token, such as qwen-max.
	PreserveEffortTail func(modelName string) bool
}

// HostedToolCapabilities declares which provider-hosted tools the target
// channel has explicitly opted into. Ordinary function tools remain available
// when a hosted interpretation is disabled.
type HostedToolCapabilities struct {
	WebSearch       bool
	GoogleSearch    bool
	FileSearch      bool
	CodeExecution   bool
	URLContext      bool
	ImageGeneration bool
	MCP             bool
}

func (c HostedToolCapabilities) Supports(toolName string) bool {
	switch strings.TrimSpace(toolName) {
	case "web_search", "web_search_preview", "web_search_options":
		return c.WebSearch
	case "googleSearch", "google_search":
		return c.GoogleSearch
	case "file_search":
		return c.FileSearch
	case "codeExecution", "code_execution":
		return c.CodeExecution
	case "urlContext", "url_context":
		return c.URLContext
	case "image_generation", "image_generation_call":
		return c.ImageGeneration
	case "mcp", "mcp_call":
		return c.MCP
	default:
		return false
	}
}

func (o *Options) SupportsHostedTool(toolName string) bool {
	return o != nil && o.HostedTools.Supports(toolName)
}

type ClaudeOptions struct {
	// ThinkingAdapterEnabled turns "-thinking"-suffixed OpenAI model names
	// into Claude extended-thinking requests.
	ThinkingAdapterEnabled bool
	// ThinkingAdapterBudgetTokensPercentage sizes thinking budget_tokens as a
	// fraction of max_tokens when the adapter fires.
	ThinkingAdapterBudgetTokensPercentage float64
	// DefaultMaxTokens returns the max_tokens to inject when the source
	// request carries none. The Claude Messages API requires max_tokens
	// (omitting it is a 400), so when this hook is nil and no other path
	// supplies a value, OpenAI→Claude request conversion fails with an
	// explicit error instead of emitting a request the upstream is
	// guaranteed to reject. The new-api host always provides this hook;
	// standalone relaykit users must supply one or guarantee max_tokens on
	// every request.
	DefaultMaxTokens func(modelName string) int
	// WebSearchToolVersion selects the Claude hosted web-search tool version
	// emitted by cross-protocol conversion. Empty keeps the compatibility
	// baseline web_search_20250305.
	WebSearchToolVersion string
}

type GeminiOptions struct {
	// ThinkingAdapterEnabled maps -thinking/-nothinking/effort suffixes to
	// Gemini thinkingConfig.
	ThinkingAdapterEnabled bool
	// ThinkingAdapterBudgetTokensPercentage sizes thinkingBudget as a fraction
	// of maxOutputTokens when the adapter fires.
	ThinkingAdapterBudgetTokensPercentage float64
	// FunctionCallThoughtSignatureEnabled attaches thoughtSignature bypass
	// values to function-call parts.
	FunctionCallThoughtSignatureEnabled bool
	// SupportsImagine reports whether the model supports image generation
	// (switches response modalities). Nil means "never".
	SupportsImagine func(modelName string) bool
	// SafetySetting returns the harm threshold for a category. Nil or empty
	// return means no safetySettings are attached.
	SafetySetting func(category string) string
}

func (o *ClaudeOptions) DefaultMaxTokensFor(modelName string) (int, bool) {
	if o == nil || o.DefaultMaxTokens == nil {
		return 0, false
	}
	return o.DefaultMaxTokens(modelName), true
}

func (o *GeminiOptions) SupportsImagineModel(modelName string) bool {
	return o != nil && o.SupportsImagine != nil && o.SupportsImagine(modelName)
}

func (o *GeminiOptions) SafetySettingFor(category string) string {
	if o == nil || o.SafetySetting == nil {
		return ""
	}
	return o.SafetySetting(category)
}

func (o *Options) ShouldPreserveThinkingSuffix(modelName string) bool {
	return o != nil && o.PreserveThinkingSuffix != nil && o.PreserveThinkingSuffix(modelName)
}

func (o *Options) ShouldPreserveEffortTail(modelName string) bool {
	return o != nil && o.PreserveEffortTail != nil && o.PreserveEffortTail(modelName)
}

func (o *Options) EffectiveToolLossPolicy() types.ConversionLossPolicy {
	if o == nil || o.ToolLossPolicy == "" {
		return types.ConversionLossPolicyAllow
	}
	return o.ToolLossPolicy
}
