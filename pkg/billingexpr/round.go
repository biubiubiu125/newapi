package billingexpr

import "github.com/QuantumNous/new-api/common"

// QuotaRound converts a float64 quota value to int using half-away-from-zero
// rounding with int32 saturation. Every tiered billing path (pre-consume,
// settlement, breakdown validation, log fields) MUST use this function to
// avoid +-1 discrepancies.
//
// It delegates to common.QuotaRound so all quota rounding/conversion shares
// one saturation + logging policy (see common/quota_math.go).
func QuotaRound(f float64) int {
	return common.QuotaRound(f)
}

// QuotaRoundPositive preserves a non-zero charge for tiny positive tiered
// expression outputs.
func QuotaRoundPositive(f float64) int {
	return common.QuotaRoundPositive(f)
}

// QuotaRoundStrict rejects an unrepresentable pre-consume estimate.
func QuotaRoundStrict(f float64) (int, error) {
	return common.QuotaRoundStrict(f)
}

// QuotaRoundPositiveStrict rejects an unrepresentable pre-consume estimate
// while preserving a non-zero charge for tiny positive tiered outputs.
func QuotaRoundPositiveStrict(f float64) (int, error) {
	return common.QuotaRoundPositiveStrict(f)
}
