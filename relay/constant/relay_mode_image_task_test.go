package constant

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPath2RelayModeRecognizesPublicImageTaskCreatePaths(t *testing.T) {
	require.Equal(t, RelayModeImagesGenerations, Path2RelayMode("/v1/image-tasks/generations"))
	require.Equal(t, RelayModeImagesEdits, Path2RelayMode("/v1/image-tasks/edits"))
}
