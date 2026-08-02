package relay

import "github.com/QuantumNous/new-api/common"

func SupportsResponsesCompactAPIType(apiType int) bool {
	return common.IsResponsesCompactAPIType(apiType)
}
