package gemini

import (
	"slices"
	"strings"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

// geminiGroundingCounter accumulates Google Search grounding across one
// upstream response. Gemini 3 bills each unique non-empty search query.
// Gemini 2.5 and older bill once per grounded prompt. Both quantities are
// recorded; the tool price table decides which one costs money.
type geminiGroundingCounter struct {
	queries map[string]struct{}
}

func (g *geminiGroundingCounter) observe(response *dto.GeminiChatResponse) {
	if g == nil || response == nil {
		return
	}
	for _, candidate := range response.Candidates {
		metadata := candidate.GroundingMetadata
		if metadata == nil {
			continue
		}
		for _, query := range slices.Concat(metadata.WebSearchQueries, metadata.ImageSearchQueries) {
			query = strings.TrimSpace(query)
			if query == "" {
				continue
			}
			if g.queries == nil {
				g.queries = make(map[string]struct{})
			}
			g.queries[query] = struct{}{}
		}
	}
}

func (g *geminiGroundingCounter) commit(info *relaycommon.RelayInfo) {
	if g == nil || info == nil || len(g.queries) == 0 {
		return
	}
	info.SetBillableToolCount(dto.BuildInToolGoogleSearch, len(g.queries))
	info.SetBillableToolCount(relaycommon.GoogleSearchGroundedPromptTool, 1)
}
