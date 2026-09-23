package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadFromJsonStringReplacesOnSuccess(t *testing.T) {
	m := NewRWMap[string, float64]()
	m.Set("old", 1)

	require.NoError(t, LoadFromJsonString(m, `{"keep":2}`))

	_, ok := m.Get("old")
	require.False(t, ok)
	value, ok := m.Get("keep")
	require.True(t, ok)
	require.Equal(t, 2.0, value)
}

func TestLoadFromJsonStringKeepsPreviousDataOnError(t *testing.T) {
	m := NewRWMap[string, float64]()
	m.Set("keep", 2)

	err := LoadFromJsonString(m, "{")
	require.Error(t, err)

	value, ok := m.Get("keep")
	require.True(t, ok)
	require.Equal(t, 2.0, value)
}

func TestLoadFromJsonStringWithCallbackKeepsPreviousDataOnError(t *testing.T) {
	m := NewRWMap[string, float64]()
	m.Set("keep", 2)
	called := false

	err := LoadFromJsonStringWithCallback(m, "{", func() { called = true })
	require.Error(t, err)
	require.False(t, called)

	value, ok := m.Get("keep")
	require.True(t, ok)
	require.Equal(t, 2.0, value)
}

func TestRWMapUnmarshalJSONKeepsPreviousDataOnError(t *testing.T) {
	m := NewRWMap[string, float64]()
	m.Set("keep", 2)

	err := m.UnmarshalJSON([]byte("{"))
	require.Error(t, err)

	value, ok := m.Get("keep")
	require.True(t, ok)
	require.Equal(t, 2.0, value)
}
