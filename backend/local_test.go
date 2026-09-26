package backend

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnvironmentWithout(t *testing.T) {
	environment := []string{
		"PATH=C:\\Windows\\System32",
		"PSModulePath=C:\\Program Files\\PowerShell\\Modules",
		"OTHER=value=with=equals",
		"psmodulepath=C:\\another\\module\\path",
	}

	filtered := environmentWithout(environment, "PSModulePath")

	require.Equal(t, []string{
		"PATH=C:\\Windows\\System32",
		"OTHER=value=with=equals",
	}, filtered)
}
