package service

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
)

func TestQuotaMutationAPIsAcceptInt64(t *testing.T) {
	var _ func(int, int64, bool) error = model.IncreaseUserQuota
	var _ func(int, int64, bool) error = model.DecreaseUserQuota
	var _ func(int, int64) error = model.DeltaUpdateUserQuota
	var _ func(int, int64) = model.UpdateUserUsedQuota
	var _ func(int, int64) = model.UpdateUserUsedQuotaAndRequestCount
	_ = t
}
