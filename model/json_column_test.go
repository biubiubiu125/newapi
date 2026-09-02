package model

import (
	"database/sql/driver"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJSONColumnValuersReturnTextValues(t *testing.T) {
	testCases := []struct {
		name   string
		valuer driver.Valuer
	}{
		{name: "ChannelInfo", valuer: ChannelInfo{IsMultiKey: true, MultiKeySize: 2}},
		{name: "Properties", valuer: Properties{Input: "hello"}},
		{name: "TaskPrivateData", valuer: TaskPrivateData{Key: "k"}},
		{name: "JSONValue", valuer: JSONValue(`[{"k":"v"}]`)},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			value, err := testCase.valuer.Value()
			require.NoError(t, err)
			_, ok := value.(string)
			assert.True(t, ok, "Value() must return string, got %T", value)
		})
	}
}

func TestJSONColumnScannersAcceptStringAndBytes(t *testing.T) {
	for _, input := range []any{
		`{"is_multi_key":true,"multi_key_size":2}`,
		[]byte(`{"is_multi_key":true,"multi_key_size":2}`),
	} {
		t.Run("scan", func(t *testing.T) {
			var info ChannelInfo
			require.NoError(t, info.Scan(input))
			assert.True(t, info.IsMultiKey)
			assert.Equal(t, 2, info.MultiKeySize)
		})
	}
}
