package common

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConditionRegexMode(t *testing.T) {
	data := []byte(`{"model":"gpt-4o-mini"}`)
	matched, err := checkConditions(data, "", []ConditionOperation{{
		Path:  "model",
		Mode:  "regex",
		Value: `^gpt-4o`,
	}}, "AND")
	require.NoError(t, err)
	require.True(t, matched)

	missed, err := checkConditions(data, "", []ConditionOperation{{
		Path:  "model",
		Mode:  "regex",
		Value: `^claude`,
	}}, "AND")
	require.NoError(t, err)
	require.False(t, missed)

	_, err = checkConditions(data, "", []ConditionOperation{{
		Path:  "model",
		Mode:  "regex",
		Value: `(`,
	}}, "AND")
	require.Error(t, err)

	_, err = checkConditions(data, "", []ConditionOperation{{
		Path:  "model",
		Mode:  "regex",
		Value: 1,
	}}, "AND")
	require.Error(t, err)
}
