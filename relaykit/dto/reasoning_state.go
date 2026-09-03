package dto

// ReasoningConversionState carries provider-native reasoning controls between
// in-process conversion steps. It is not part of any provider wire protocol.
type ReasoningConversionState struct {
	Mode            string
	Effort          string
	BudgetTokens    *int
	IncludeThoughts *bool
}
