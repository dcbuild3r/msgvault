package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadMatrixConfig(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	path := filepath.Join(home, "config.toml")
	require.NoError(t, os.WriteFile(path, []byte(`
[matrix]
homeserver = "https://matrix.example.test"
user_id = "@archive:example.test"
enabled = true
schedule = "* * * * *"
media = false
max_media_mb = 25
`), 0o600))

	cfg, err := Load(path, "")
	require.NoError(t, err)
	assert.Equal(t, "https://matrix.example.test", cfg.Matrix.Homeserver)
	assert.Equal(t, "@archive:example.test", cfg.Matrix.UserID)
	assert.True(t, cfg.Matrix.Enabled)
	assert.Equal(t, "* * * * *", cfg.Matrix.Schedule)
	assert.False(t, cfg.Matrix.MediaEnabled())
	assert.Equal(t, int64(25<<20), cfg.Matrix.MaxMediaBytes())
}

func TestMatrixConfigMediaDefaults(t *testing.T) {
	t.Parallel()
	cfg := MatrixConfig{}
	assert.True(t, cfg.MediaEnabled())
	assert.Equal(t, int64(100<<20), cfg.MaxMediaBytes())
}
