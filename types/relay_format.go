package types

import kittypes "github.com/QuantumNous/new-api/relaykit/types"

type RelayFormat = kittypes.RelayFormat

const (
	RelayFormatOpenAI                    = kittypes.RelayFormatOpenAI
	RelayFormatClaude                    = kittypes.RelayFormatClaude
	RelayFormatGemini                    = kittypes.RelayFormatGemini
	RelayFormatOpenAIResponses           = kittypes.RelayFormatOpenAIResponses
	RelayFormatOpenAIResponsesCompaction = kittypes.RelayFormatOpenAIResponsesCompaction
	RelayFormatOpenAIAlphaSearch         = kittypes.RelayFormatOpenAIAlphaSearch
	RelayFormatOpenAIAudio               = kittypes.RelayFormatOpenAIAudio
	RelayFormatOpenAIImage               = kittypes.RelayFormatOpenAIImage
	RelayFormatOpenAIRealtime            = kittypes.RelayFormatOpenAIRealtime
	RelayFormatRerank                    = kittypes.RelayFormatRerank
	RelayFormatEmbedding                 = kittypes.RelayFormatEmbedding
	RelayFormatTask                      = kittypes.RelayFormatTask
	RelayFormatMjProxy                   = kittypes.RelayFormatMjProxy
)
