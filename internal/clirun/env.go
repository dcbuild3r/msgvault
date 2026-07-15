package clirun

// EnvIMAPPassword names the env var used to pass an IMAP password to a daemon-owned CLI subprocess.
const EnvIMAPPassword = "MSGVAULT_IMAP_PASSWORD" // #nosec G101 -- environment variable name, not a credential value

// EnvBeeperToken names the env var used to pass a Beeper Desktop access token
// to a daemon-owned CLI subprocess.
const EnvBeeperToken = "MSGVAULT_BEEPER_TOKEN" // #nosec G101 -- environment variable name, not a credential value

// EnvMatrixToken names the env var used to pass a Matrix access token to a
// daemon-owned CLI subprocess.
const EnvMatrixToken = "MSGVAULT_MATRIX_TOKEN" // #nosec G101 -- environment variable name, not a credential value

// EnvRemoteDeleteOptIn names the env var that opts into executing staged remote deletions.
const EnvRemoteDeleteOptIn = "MSGVAULT_ENABLE_REMOTE_DELETE"

func EnvAllowed(name string) bool {
	switch name {
	case EnvIMAPPassword, EnvBeeperToken, EnvMatrixToken, EnvRemoteDeleteOptIn:
		return true
	default:
		return false
	}
}
