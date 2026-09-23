package relay

import (
	"testing"
	"unicode"

	"github.com/stretchr/testify/require"
)

func TestMidjourneyChannelDisabledMessageStaysEnglish(t *testing.T) {
	got := midjourneyChannelDisabledMessage()
	require.Equal(t, "channel_disabled", got)
	for _, r := range got {
		if unicode.In(r, unicode.Han) {
			t.Fatalf("contains Han: %q", got)
		}
	}
}
