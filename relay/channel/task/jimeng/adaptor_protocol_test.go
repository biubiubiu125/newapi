package jimeng

import (
	"testing"
	"unicode"

	"github.com/QuantumNous/new-api/i18n"
	"github.com/stretchr/testify/require"
)

func TestFileTooLargeErrorStaysEnglish(t *testing.T) {
	require.NoError(t, i18n.Init())
	err := fileTooLargeError("demo.png")
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds the size limit")
	for _, r := range err.Error() {
		if unicode.In(r, unicode.Han) {
			t.Fatalf("contains Han: %q", err.Error())
		}
	}
}
