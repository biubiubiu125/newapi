package common

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateNumericCodeReturnsLocalizedError(t *testing.T) {
	_, err := ValidateNumericCode("12")
	loc, ok := AsLocalizedError(err)
	require.True(t, ok, "length error should be LocalizedError, got %v", err)
	require.Equal(t, "twofa.code_length", loc.Key)

	_, err = ValidateNumericCode("12ab56")
	loc, ok = AsLocalizedError(err)
	require.True(t, ok, "digits error should be LocalizedError, got %v", err)
	require.Equal(t, "twofa.code_digits", loc.Key)

	got, err := ValidateNumericCode("123456")
	require.NoError(t, err)
	require.Equal(t, "123456", got)
}
