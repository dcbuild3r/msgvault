package matrix_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	msgmatrix "go.kenn.io/msgvault/internal/matrix"
)

func TestClientWhoAmIAuthenticatesAndReturnsMatrixUser(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/_matrix/client/v3/account/whoami", r.URL.Path)
		assert.Equal(t, "Bearer archive-token", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"user_id":"@archive:example.test","device_id":"MSGVAULT_ARCHIVE"}`))
	}))
	t.Cleanup(server.Close)

	client, err := msgmatrix.NewClient(server.URL, "archive-token")
	require.NoError(t, err)
	who, err := client.WhoAmI(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "@archive:example.test", who.UserID)
	assert.Equal(t, "MSGVAULT_ARCHIVE", who.DeviceID)
}
