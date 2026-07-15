package matrix

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCredentialsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := Credentials{
		Homeserver:  "https://matrix.example.test",
		UserID:      "@archive:example.test",
		AccessToken: "secret-token",
	}
	require.NoError(t, SaveCredentials(dir, want))
	got, err := LoadCredentials(dir)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}
