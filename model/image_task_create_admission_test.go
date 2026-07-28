package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestImageTaskCreateAdmissionEnforcesRateAndGlobalCapacity(t *testing.T) {
	truncateTables(t)
	now := int64(1_700_000_000)
	limits := ImageTaskCreateAdmissionLimits{
		RequestLimit:          1,
		WindowSeconds:         60,
		MaxInFlight:           1,
		MaxReservedBytes:      128,
		ReservationTTLSeconds: 600,
	}

	first, err := AcquireImageTaskCreateAdmission(101, 201, 64, now, limits)
	require.NoError(t, err)
	require.NotEmpty(t, first)

	_, err = AcquireImageTaskCreateAdmission(101, 202, 64, now, limits)
	require.ErrorIs(t, err, ErrImageTaskCreateRateLimitExceeded)

	_, err = AcquireImageTaskCreateAdmission(102, 203, 64, now, limits)
	require.ErrorIs(t, err, ErrImageTaskCreateCapacityExceeded)

	require.NoError(t, ReleaseImageTaskCreateAdmission(first))
	second, err := AcquireImageTaskCreateAdmission(102, 203, 64, now, limits)
	require.NoError(t, err)
	require.NotEmpty(t, second)
	require.NoError(t, ReleaseImageTaskCreateAdmission(second))
}

func TestImageTaskCreateAdmissionReclaimsExpiredReservation(t *testing.T) {
	truncateTables(t)
	limits := ImageTaskCreateAdmissionLimits{
		MaxInFlight:           1,
		MaxReservedBytes:      128,
		ReservationTTLSeconds: 10,
	}

	first, err := AcquireImageTaskCreateAdmission(301, 401, 128, 1_700_000_000, limits)
	require.NoError(t, err)
	require.NotEmpty(t, first)

	second, err := AcquireImageTaskCreateAdmission(302, 402, 128, 1_700_000_011, limits)
	require.NoError(t, err)
	require.NotEmpty(t, second)
	require.NoError(t, ReleaseImageTaskCreateAdmission(second))
}

func TestRenewImageTaskCreateAdmissionKeepsActiveReservationCounted(t *testing.T) {
	truncateTables(t)
	limits := ImageTaskCreateAdmissionLimits{
		MaxInFlight:           1,
		MaxReservedBytes:      128,
		ReservationTTLSeconds: 10,
	}
	startedAt := int64(1_700_000_000)

	first, err := AcquireImageTaskCreateAdmission(501, 601, 128, startedAt, limits)
	require.NoError(t, err)
	require.NotEmpty(t, first)

	renewed, err := RenewImageTaskCreateAdmission(first, startedAt+9, limits.ReservationTTLSeconds)
	require.NoError(t, err)
	require.True(t, renewed)

	_, err = AcquireImageTaskCreateAdmission(502, 602, 128, startedAt+11, limits)
	require.ErrorIs(t, err, ErrImageTaskCreateCapacityExceeded)

	second, err := AcquireImageTaskCreateAdmission(502, 602, 128, startedAt+20, limits)
	require.NoError(t, err)
	require.NotEmpty(t, second)
	require.NoError(t, ReleaseImageTaskCreateAdmission(second))
}
