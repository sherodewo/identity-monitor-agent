package model

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestRunTransitions(t *testing.T) {
	require.True(t, RunTransition("PENDING", "RUNNING"))
	require.True(t, RunTransition("RUNNING", "HUMAN_ACTION_REQUIRED"))
	require.True(t, RunTransition("PAUSED", "PENDING"))
	require.False(t, RunTransition("COMPLETED", "RUNNING"))
	require.False(t, RunTransition("PENDING", "APPROVED"))
}
func TestCaseTransitions(t *testing.T) {
	path := []string{"OPEN", "WAITING_APPROVAL", "APPROVED", "SUBMITTED_MANUALLY", "REMOVED", "CLOSED"}
	for i := 1; i < len(path); i++ {
		require.True(t, CaseTransition(path[i-1], path[i]))
	}
	require.False(t, CaseTransition("OPEN", "APPROVED"))
	require.False(t, CaseTransition("REJECTED", "SUBMITTED_MANUALLY"))
	require.False(t, CaseTransition("CLOSED", "OPEN"))
}
