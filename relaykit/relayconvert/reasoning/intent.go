package reasoning

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

type Effort string

const (
	EffortNone    Effort = "none"
	EffortMinimal Effort = "minimal"
	EffortLow     Effort = "low"
	EffortMedium  Effort = "medium"
	EffortHigh    Effort = "high"
	EffortXHigh   Effort = "xhigh"
	EffortMax     Effort = "max"
)

type Mode string
type Source string

const (
	ModeUnset    Mode = ""
	ModeEnabled  Mode = "enabled"
	ModeAdaptive Mode = "adaptive"
	ModeDisabled Mode = "disabled"
)

const (
	SourceExplicit Source = "explicit"
	SourceNative   Source = "native"
	SourceSuffix   Source = "suffix"
	SourcePivot    Source = "pivot"
)

var (
	ErrEffortConflict      = errors.New("reasoning settings conflict")
	ErrUnsupportedEffort   = errors.New("unsupported reasoning effort")
	ErrThinkingNotDisabled = errors.New("thinking cannot be disabled")
)

// ClientError marks invalid request controls so callers can return a 4xx.
type ClientError struct{ err error }

func (e *ClientError) Error() string { return e.err.Error() }
func (e *ClientError) Unwrap() error { return e.err }

func AsClientError(err error) error {
	if err == nil {
		return nil
	}
	var clientErr *ClientError
	if errors.As(err, &clientErr) {
		return err
	}
	return &ClientError{err: err}
}

func IsClientError(err error) bool {
	var clientErr *ClientError
	return errors.As(err, &clientErr)
}

// Intent is the provider-neutral reasoning control used only during
// conversion. The original request fields remain available for billing and
// passthrough decisions.
type Intent struct {
	Mode            Mode
	Effort          Effort
	BudgetTokens    *int
	IncludeThoughts *bool
	Source          Source
	BudgetSource    Source
}

func (i Intent) HasStrength() bool {
	return i.Mode != ModeUnset || i.Effort != "" || i.BudgetTokens != nil
}

func (i Intent) IsEmpty() bool {
	return !i.HasStrength() && i.IncludeThoughts == nil
}

func IntentFromState(state *dto.ReasoningConversionState) Intent {
	if state == nil {
		return Intent{}
	}
	return Intent{
		Mode:            Mode(state.Mode),
		Effort:          Effort(state.Effort),
		BudgetTokens:    state.BudgetTokens,
		IncludeThoughts: state.IncludeThoughts,
		Source:          SourcePivot,
		BudgetSource:    SourcePivot,
	}
}

func StateFromIntent(intent Intent) *dto.ReasoningConversionState {
	if intent.IsEmpty() {
		return nil
	}
	return &dto.ReasoningConversionState{
		Mode:            string(intent.Mode),
		Effort:          string(intent.Effort),
		BudgetTokens:    intent.BudgetTokens,
		IncludeThoughts: intent.IncludeThoughts,
	}
}

func ParseEffort(value string) (Effort, error) {
	effort := Effort(strings.ToLower(strings.TrimSpace(value)))
	switch effort {
	case "":
		return "", nil
	case EffortNone, EffortMinimal, EffortLow, EffortMedium, EffortHigh, EffortXHigh, EffortMax:
		return effort, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnsupportedEffort, value)
	}
}

func normalizeIntent(intent Intent) (Intent, error) {
	effort, err := ParseEffort(string(intent.Effort))
	if err != nil {
		return Intent{}, err
	}
	intent.Effort = effort

	switch intent.Mode {
	case ModeUnset, ModeEnabled, ModeAdaptive, ModeDisabled:
	default:
		return Intent{}, fmt.Errorf("unsupported reasoning mode %q", intent.Mode)
	}

	if intent.BudgetTokens != nil {
		budget := *intent.BudgetTokens
		if budget < -1 {
			return Intent{}, fmt.Errorf("thinking budget must be -1 or non-negative, got %d", budget)
		}
		if budget == 0 {
			if intent.Mode == ModeEnabled || intent.Mode == ModeAdaptive ||
				(intent.Effort != "" && intent.Effort != EffortNone) {
				return Intent{}, fmt.Errorf("%w: zero budget disables thinking", ErrEffortConflict)
			}
			intent.Mode = ModeDisabled
			intent.Effort = EffortNone
		} else if intent.Mode == ModeDisabled || intent.Effort == EffortNone {
			return Intent{}, fmt.Errorf("%w: a non-zero budget enables thinking", ErrEffortConflict)
		} else if intent.Mode == ModeUnset {
			intent.Mode = ModeEnabled
		}
	}

	if intent.Effort == EffortNone {
		if intent.Mode == ModeEnabled || intent.Mode == ModeAdaptive {
			return Intent{}, fmt.Errorf("%w: effort none disables thinking", ErrEffortConflict)
		}
		intent.Mode = ModeDisabled
	}
	return intent, nil
}

func MergeExplicitAndSuffix(explicit Intent, suffix Intent, model string) (Intent, error) {
	var err error
	explicit, err = normalizeIntent(explicit)
	if err != nil {
		return Intent{}, err
	}
	suffix, err = normalizeIntent(suffix)
	if err != nil {
		return Intent{}, err
	}
	if !explicit.HasStrength() {
		if explicit.IncludeThoughts != nil {
			suffix.IncludeThoughts = explicit.IncludeThoughts
		}
		return suffix, nil
	}
	if !suffix.HasStrength() {
		if explicit.IncludeThoughts == nil {
			explicit.IncludeThoughts = suffix.IncludeThoughts
		}
		return explicit, nil
	}

	explicitDisabled := explicit.Mode == ModeDisabled || explicit.Effort == EffortNone
	suffixDisabled := suffix.Mode == ModeDisabled || suffix.Effort == EffortNone
	if explicitDisabled != suffixDisabled {
		return Intent{}, fmt.Errorf("%w for model %q: explicit and suffix disagree about enabled state", ErrEffortConflict, model)
	}
	if !explicitDisabled && explicit.Effort != "" && suffix.Effort != "" && explicit.Effort != suffix.Effort {
		return Intent{}, fmt.Errorf("%w for model %q: explicit effort %q differs from suffix effort %q", ErrEffortConflict, model, explicit.Effort, suffix.Effort)
	}
	if explicit.BudgetTokens != nil && suffix.BudgetTokens != nil &&
		*explicit.BudgetTokens != *suffix.BudgetTokens {
		return Intent{}, fmt.Errorf("%w for model %q: explicit budget differs from suffix budget", ErrEffortConflict, model)
	}
	if (explicit.Effort != "" && explicit.Effort != EffortNone && suffix.BudgetTokens != nil) ||
		(explicit.BudgetTokens != nil && suffix.Effort != "" && suffix.Effort != EffortNone) {
		return Intent{}, fmt.Errorf("%w for model %q: effort and exact suffix budget conflict", ErrEffortConflict, model)
	}

	merged := suffix
	if explicit.Mode != ModeUnset {
		merged.Mode = explicit.Mode
	}
	if explicit.Effort != "" {
		merged.Effort = explicit.Effort
	}
	if explicit.BudgetTokens != nil {
		merged.BudgetTokens = explicit.BudgetTokens
		merged.BudgetSource = explicit.BudgetSource
	}
	if explicit.IncludeThoughts != nil {
		merged.IncludeThoughts = explicit.IncludeThoughts
	}
	return normalizeIntent(merged)
}

// MergeExplicit combines two structured representations of one request.
// Effort and budget are allowed together because some providers expose both.
func MergeExplicit(primary Intent, secondary Intent, model string) (Intent, error) {
	var err error
	primary, err = normalizeIntent(primary)
	if err != nil {
		return Intent{}, err
	}
	secondary, err = normalizeIntent(secondary)
	if err != nil {
		return Intent{}, err
	}
	if primary.IsEmpty() {
		return secondary, nil
	}
	if secondary.IsEmpty() {
		return primary, nil
	}

	primaryDisabled := primary.Mode == ModeDisabled || primary.Effort == EffortNone
	secondaryDisabled := secondary.Mode == ModeDisabled || secondary.Effort == EffortNone
	if primary.HasStrength() && secondary.HasStrength() && primaryDisabled != secondaryDisabled {
		return Intent{}, fmt.Errorf("%w for model %q: explicit fields disagree about enabled state", ErrEffortConflict, model)
	}
	if primary.Effort != "" && secondary.Effort != "" && primary.Effort != secondary.Effort {
		return Intent{}, fmt.Errorf("%w for model %q: explicit efforts differ", ErrEffortConflict, model)
	}
	if primary.BudgetTokens != nil && secondary.BudgetTokens != nil &&
		*primary.BudgetTokens != *secondary.BudgetTokens {
		return Intent{}, fmt.Errorf("%w for model %q: explicit budgets differ", ErrEffortConflict, model)
	}

	merged := secondary
	if primary.Mode != ModeUnset {
		merged.Mode = primary.Mode
	}
	if primary.Effort != "" {
		merged.Effort = primary.Effort
	}
	if primary.BudgetTokens != nil {
		merged.BudgetTokens = primary.BudgetTokens
		merged.BudgetSource = primary.BudgetSource
	}
	if primary.IncludeThoughts != nil {
		merged.IncludeThoughts = primary.IncludeThoughts
	}
	return normalizeIntent(merged)
}

type openRouterReasoning struct {
	Enabled   *bool  `json:"enabled"`
	Effort    string `json:"effort,omitempty"`
	MaxTokens *int   `json:"max_tokens,omitempty"`
	Exclude   *bool  `json:"exclude,omitempty"`
}

func FromOpenAIChat(req *dto.GeneralOpenAIRequest) (Intent, error) {
	if req == nil {
		return Intent{}, nil
	}
	var intent Intent
	intent.Source = SourceExplicit
	if req.ReasoningEffort != "" {
		effort, err := ParseEffort(req.ReasoningEffort)
		if err != nil {
			return Intent{}, err
		}
		intent.Effort = effort
		if effort == EffortNone {
			intent.Mode = ModeDisabled
		} else {
			intent.Mode = ModeEnabled
		}
	}
	if len(req.Reasoning) > 0 {
		var raw openRouterReasoning
		if err := kitutil.Unmarshal(req.Reasoning, &raw); err != nil {
			return Intent{}, fmt.Errorf("invalid reasoning config: %w", err)
		}
		nested := Intent{
			Source:       SourceExplicit,
			BudgetSource: SourceExplicit,
			BudgetTokens: raw.MaxTokens,
		}
		if raw.Enabled != nil {
			if *raw.Enabled {
				nested.Mode = ModeEnabled
			} else {
				nested.Mode = ModeDisabled
				nested.Effort = EffortNone
			}
		}
		if raw.Effort != "" {
			effort, err := ParseEffort(raw.Effort)
			if err != nil {
				return Intent{}, err
			}
			nested.Effort = effort
			if effort == EffortNone {
				nested.Mode = ModeDisabled
			} else if nested.Mode == ModeUnset {
				nested.Mode = ModeEnabled
			}
		}
		if raw.Exclude != nil {
			include := !*raw.Exclude
			nested.IncludeThoughts = &include
		}
		var mergeErr error
		intent, mergeErr = MergeExplicit(intent, nested, req.Model)
		if mergeErr != nil {
			return Intent{}, mergeErr
		}
	}
	if req.ReasoningConversion != nil {
		pivot := IntentFromState(req.ReasoningConversion)
		pivot.Source = SourcePivot
		pivot.BudgetSource = SourcePivot
		return MergeExplicit(intent, pivot, req.Model)
	}
	return normalizeIntent(intent)
}

func ApplyToOpenAIChat(req *dto.GeneralOpenAIRequest, intent Intent) error {
	if req == nil {
		return nil
	}
	var err error
	intent, err = normalizeIntent(intent)
	if err != nil {
		return err
	}
	if effort := OpenAIEffort(EffectiveEffort(intent)); effort != "" {
		req.ReasoningEffort = string(effort)
	}
	req.ReasoningConversion = StateFromIntent(intent)
	return nil
}

func ApplyToOpenAIResponses(req *dto.OpenAIResponsesRequest, intent Intent) error {
	if req == nil {
		return nil
	}
	var err error
	intent, err = normalizeIntent(intent)
	if err != nil {
		return err
	}
	if effort := OpenAIEffort(EffectiveEffort(intent)); effort != "" {
		summary := "detailed"
		if effort == EffortNone || (intent.IncludeThoughts != nil && !*intent.IncludeThoughts) {
			summary = ""
		}
		req.Reasoning = &dto.Reasoning{Effort: string(effort), Summary: summary}
	}
	req.ReasoningConversion = StateFromIntent(intent)
	return nil
}

func ApplyToClaude(req *dto.ClaudeRequest, intent Intent) error {
	if req == nil {
		return nil
	}
	var err error
	intent, err = normalizeIntent(intent)
	if err != nil {
		return err
	}
	if intent.IsEmpty() {
		return nil
	}
	if intent.Mode == ModeDisabled || intent.Effort == EffortNone {
		req.Thinking = nil
		req.OutputConfig = nil
		return nil
	}

	if intent.Mode == ModeAdaptive {
		req.Thinking = &dto.Thinking{Type: string(ModeAdaptive)}
		if intent.IncludeThoughts != nil && *intent.IncludeThoughts {
			req.Thinking.Display = "summarized"
		}
		if intent.Effort != "" {
			req.OutputConfig, err = kitutil.Marshal(map[string]string{
				"effort": string(intent.Effort),
			})
			if err != nil {
				return fmt.Errorf("marshal Claude output_config: %w", err)
			}
		}
		return nil
	}

	budget := intent.BudgetTokens
	if budget == nil {
		value := claudeBudgetForEffort(intent.Effort)
		budget = &value
	}
	req.Thinking = &dto.Thinking{
		Type:         string(ModeEnabled),
		BudgetTokens: budget,
	}
	return nil
}

func claudeBudgetForEffort(effort Effort) int {
	switch effort {
	case EffortMinimal, EffortLow:
		return 1280
	case EffortMedium:
		return 2048
	case EffortHigh, EffortXHigh, EffortMax:
		return 8192
	default:
		return 4096
	}
}

func OpenAIEffort(effort Effort) Effort {
	if effort == EffortMax {
		return EffortXHigh
	}
	return effort
}

func FromOpenAIResponses(req *dto.OpenAIResponsesRequest) (Intent, error) {
	if req == nil {
		return Intent{}, nil
	}
	var intent Intent
	if req.Reasoning != nil {
		intent.Source = SourceExplicit
		if req.Reasoning.Effort != "" {
			effort, err := ParseEffort(req.Reasoning.Effort)
			if err != nil {
				return Intent{}, err
			}
			intent.Effort = effort
			if effort == EffortNone {
				intent.Mode = ModeDisabled
			} else {
				intent.Mode = ModeEnabled
			}
		}
		if req.Reasoning.Summary != "" {
			include := true
			intent.IncludeThoughts = &include
		}
	}
	if req.ReasoningConversion != nil {
		pivot := IntentFromState(req.ReasoningConversion)
		pivot.Source = SourcePivot
		pivot.BudgetSource = SourcePivot
		return MergeExplicit(intent, pivot, req.Model)
	}
	return normalizeIntent(intent)
}

func FromClaude(req *dto.ClaudeRequest) (Intent, error) {
	if req == nil {
		return Intent{}, nil
	}
	var intent Intent
	intent.Source = SourceNative
	if req.Thinking != nil {
		switch req.Thinking.Type {
		case "", "enabled":
			intent.Mode = ModeEnabled
		case "adaptive":
			intent.Mode = ModeAdaptive
		case "disabled":
			intent.Mode = ModeDisabled
			intent.Effort = EffortNone
		default:
			return Intent{}, fmt.Errorf("unsupported Claude thinking type %q", req.Thinking.Type)
		}
		intent.BudgetTokens = req.Thinking.BudgetTokens
		intent.BudgetSource = SourceNative
		switch req.Thinking.Display {
		case "summarized":
			value := true
			intent.IncludeThoughts = &value
		case "omitted":
			value := false
			intent.IncludeThoughts = &value
		}
	}
	if len(req.OutputConfig) > 0 {
		var output dto.OutputConfigForEffort
		if err := kitutil.Unmarshal(req.OutputConfig, &output); err != nil {
			return Intent{}, fmt.Errorf("invalid Claude output_config: %w", err)
		}
		if output.Effort != "" {
			effort, err := ParseEffort(output.Effort)
			if err != nil {
				return Intent{}, err
			}
			intent.Effort = effort
			if intent.Mode == ModeUnset {
				intent.Mode = ModeEnabled
			}
		}
	}
	return normalizeIntent(intent)
}

func FromGemini(req *dto.GeminiChatRequest) (Intent, error) {
	if req == nil || req.GenerationConfig.ThinkingConfig == nil {
		return Intent{}, nil
	}
	config := req.GenerationConfig.ThinkingConfig
	if config.ThinkingBudget != nil && config.ThinkingLevel != "" {
		return Intent{}, fmt.Errorf("%w: Gemini thinkingBudget and thinkingLevel cannot both be set", ErrEffortConflict)
	}
	intent := Intent{
		BudgetTokens: config.ThinkingBudget,
		Source:       SourceNative,
		BudgetSource: SourceNative,
	}
	if config.IncludeThoughts {
		includeThoughts := true
		intent.IncludeThoughts = &includeThoughts
	}
	if config.ThinkingLevel != "" {
		effort, err := ParseEffort(config.ThinkingLevel)
		if err != nil {
			return Intent{}, err
		}
		intent.Effort = effort
		intent.Mode = ModeEnabled
	}
	return normalizeIntent(intent)
}

func EffectiveEffort(intent Intent) Effort {
	intent, err := normalizeIntent(intent)
	if err != nil {
		return ""
	}
	if intent.Mode == ModeDisabled {
		return EffortNone
	}
	if intent.Effort != "" {
		return intent.Effort
	}
	if intent.BudgetTokens != nil {
		return EffortFromBudget(*intent.BudgetTokens)
	}
	if intent.Mode == ModeEnabled || intent.Mode == ModeAdaptive {
		return EffortHigh
	}
	return ""
}

func EffortFromBudget(budget int) Effort {
	switch {
	case budget == 0:
		return EffortNone
	case budget < 0:
		return EffortHigh
	case budget <= 1024:
		return EffortLow
	case budget <= 8192:
		return EffortMedium
	default:
		return EffortHigh
	}
}
