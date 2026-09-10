package common

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestJsonRawMessageToString(t *testing.T) {
	tests := []struct {
		name string
		data json.RawMessage
		want string
	}{
		{
			name: "object",
			data: json.RawMessage(`{"city":"Paris","days":0,"strict":false}`),
			want: `{"city":"Paris","days":0,"strict":false}`,
		},
		{
			name: "string",
			data: json.RawMessage(`"{\"city\":\"Paris\",\"days\":0,\"strict\":false}"`),
			want: `{"city":"Paris","days":0,"strict":false}`,
		},
		{
			name: "null",
			data: json.RawMessage(`null`),
			want: "",
		},
		{
			name: "empty",
			data: nil,
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, JsonRawMessageToString(tt.data))
		})
	}
}

func TestDecodeJsonRejectsTrailingValues(t *testing.T) {
	var value map[string]any
	require.Error(t, DecodeJson(bytes.NewBufferString(`{"ok":true}{"extra":true}`), &value))
	require.NoError(t, DecodeJson(bytes.NewBufferString("{\"ok\":true} \n\t"), &value))
}

func TestHostJSONCodecSortsMapKeys(t *testing.T) {
	encoded, err := Marshal(map[string]any{"z": 1, "a": 2, "m": 3})
	require.NoError(t, err)
	require.Equal(t, `{"a":2,"m":3,"z":1}`, string(encoded))
}
