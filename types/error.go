package types

import kittypes "github.com/QuantumNous/new-api/relaykit/types"

type OpenAIError = kittypes.OpenAIError
type ClaudeError = kittypes.ClaudeError
type ErrorType = kittypes.ErrorType
type ErrorCode = kittypes.ErrorCode
type NewAPIError = kittypes.NewAPIError
type NewAPIErrorOptions = kittypes.NewAPIErrorOptions

const (
	ErrorTypeNewAPIError     = kittypes.ErrorTypeNewAPIError
	ErrorTypeOpenAIError     = kittypes.ErrorTypeOpenAIError
	ErrorTypeClaudeError     = kittypes.ErrorTypeClaudeError
	ErrorTypeMidjourneyError = kittypes.ErrorTypeMidjourneyError
	ErrorTypeGeminiError     = kittypes.ErrorTypeGeminiError
	ErrorTypeRerankError     = kittypes.ErrorTypeRerankError
	ErrorTypeUpstreamError   = kittypes.ErrorTypeUpstreamError
)

const (
	ErrorCodeInvalidRequest         = kittypes.ErrorCodeInvalidRequest
	ErrorCodeSensitiveWordsDetected = kittypes.ErrorCodeSensitiveWordsDetected
	ErrorCodeViolationFeeGrokCSAM   = kittypes.ErrorCodeViolationFeeGrokCSAM

	ErrorCodeCountTokenFailed   = kittypes.ErrorCodeCountTokenFailed
	ErrorCodeModelPriceError    = kittypes.ErrorCodeModelPriceError
	ErrorCodeInvalidApiType     = kittypes.ErrorCodeInvalidApiType
	ErrorCodeJsonMarshalFailed  = kittypes.ErrorCodeJsonMarshalFailed
	ErrorCodeDoRequestFailed    = kittypes.ErrorCodeDoRequestFailed
	ErrorCodeGetChannelFailed   = kittypes.ErrorCodeGetChannelFailed
	ErrorCodeGenRelayInfoFailed = kittypes.ErrorCodeGenRelayInfoFailed

	ErrorCodeChannelNoAvailableKey        = kittypes.ErrorCodeChannelNoAvailableKey
	ErrorCodeChannelParamOverrideInvalid  = kittypes.ErrorCodeChannelParamOverrideInvalid
	ErrorCodeChannelHeaderOverrideInvalid = kittypes.ErrorCodeChannelHeaderOverrideInvalid
	ErrorCodeChannelModelMappedError      = kittypes.ErrorCodeChannelModelMappedError
	ErrorCodeChannelAwsClientError        = kittypes.ErrorCodeChannelAwsClientError
	ErrorCodeChannelInvalidKey            = kittypes.ErrorCodeChannelInvalidKey
	ErrorCodeChannelResponseTimeExceeded  = kittypes.ErrorCodeChannelResponseTimeExceeded

	ErrorCodeReadRequestBodyFailed = kittypes.ErrorCodeReadRequestBodyFailed
	ErrorCodeConvertRequestFailed  = kittypes.ErrorCodeConvertRequestFailed
	ErrorCodeAccessDenied          = kittypes.ErrorCodeAccessDenied

	ErrorCodeBadRequestBody = kittypes.ErrorCodeBadRequestBody

	ErrorCodeReadResponseBodyFailed = kittypes.ErrorCodeReadResponseBodyFailed
	ErrorCodeBadResponseStatusCode  = kittypes.ErrorCodeBadResponseStatusCode
	ErrorCodeBadResponse            = kittypes.ErrorCodeBadResponse
	ErrorCodeBadResponseBody        = kittypes.ErrorCodeBadResponseBody
	ErrorCodeEmptyResponse          = kittypes.ErrorCodeEmptyResponse
	ErrorCodeAwsInvokeError         = kittypes.ErrorCodeAwsInvokeError
	ErrorCodeModelNotFound          = kittypes.ErrorCodeModelNotFound
	ErrorCodePromptBlocked          = kittypes.ErrorCodePromptBlocked

	ErrorCodeQueryDataError  = kittypes.ErrorCodeQueryDataError
	ErrorCodeUpdateDataError = kittypes.ErrorCodeUpdateDataError

	ErrorCodeInsufficientUserQuota      = kittypes.ErrorCodeInsufficientUserQuota
	ErrorCodePreConsumeTokenQuotaFailed = kittypes.ErrorCodePreConsumeTokenQuotaFailed

	ErrorCodeIdempotencyConflict   = kittypes.ErrorCodeIdempotencyConflict
	ErrorCodeIdempotencyInProgress = kittypes.ErrorCodeIdempotencyInProgress
)

var (
	NewError                      = kittypes.NewError
	NewOpenAIError                = kittypes.NewOpenAIError
	InitOpenAIError               = kittypes.InitOpenAIError
	NewErrorWithStatusCode        = kittypes.NewErrorWithStatusCode
	WithOpenAIError               = kittypes.WithOpenAIError
	WithClaudeError               = kittypes.WithClaudeError
	IsChannelError                = kittypes.IsChannelError
	IsSkipRetryError              = kittypes.IsSkipRetryError
	ErrOptionWithSkipRetry        = kittypes.ErrOptionWithSkipRetry
	ErrOptionWithNoRecordErrorLog = kittypes.ErrOptionWithNoRecordErrorLog
	ErrOptionWithStatusCode       = kittypes.ErrOptionWithStatusCode
	ErrOptionWithHideErrMsg       = kittypes.ErrOptionWithHideErrMsg
	IsRecordErrorLog              = kittypes.IsRecordErrorLog
)
