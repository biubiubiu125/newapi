package common

import (
	"errors"
	"fmt"
	"math"
	"strings"
)

func NormalizeReferralSettlementCurrency(value string) string {
	currency := strings.ToUpper(strings.TrimSpace(value))
	if currency != "CNY" {
		return "CNY"
	}
	return currency
}

func NormalizeReferralFxRates(input map[string]float64) (map[string]float64, error) {
	out := map[string]float64{"CNY": 1}
	for rawCurrency, rawRate := range input {
		currency := strings.ToUpper(strings.TrimSpace(rawCurrency))
		if currency == "" {
			continue
		}
		if math.IsNaN(rawRate) || math.IsInf(rawRate, 0) || rawRate <= 0 {
			return nil, fmt.Errorf("referral fx rate for %s must be positive", currency)
		}
		if currency == "CNY" {
			out["CNY"] = 1
			continue
		}
		out[currency] = rawRate
	}
	return out, nil
}

func ReferralSettlementFxRatesToJSONString() string {
	normalized, err := NormalizeReferralFxRates(ReferralSettlementFxRates)
	if err != nil {
		normalized = map[string]float64{"CNY": 1}
	}
	bytes, err := Marshal(normalized)
	if err != nil {
		return `{"CNY":1}`
	}
	return string(bytes)
}

func ParseReferralSettlementFxRatesJSONString(value string) (map[string]float64, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return map[string]float64{"CNY": 1}, nil
	}
	var parsed map[string]float64
	if err := UnmarshalJsonStr(trimmed, &parsed); err != nil {
		return nil, errors.New("referral fx rates must be a JSON object")
	}
	return NormalizeReferralFxRates(parsed)
}

func UpdateReferralSettlementFxRatesByJSONString(value string) error {
	normalized, err := ParseReferralSettlementFxRatesJSONString(value)
	if err != nil {
		return err
	}
	ReferralSettlementFxRates = normalized
	return nil
}

func ReferralSettlementFxRatesSnapshot() map[string]float64 {
	normalized, err := NormalizeReferralFxRates(ReferralSettlementFxRates)
	if err != nil {
		normalized = map[string]float64{"CNY": 1}
	}
	out := make(map[string]float64, len(normalized))
	for currency, rate := range normalized {
		out[currency] = rate
	}
	return out
}
