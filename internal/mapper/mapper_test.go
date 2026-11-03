package mapper_test

import (
	"path/filepath"
	"testing"

	"github.com/cslemes/balancas_consinco/internal/mapper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFolderMapper_MatchFolder(t *testing.T) {
	m, err := mapper.NewFolderMapper(`(?i)^bal(\d+)$`)
	require.NoError(t, err)

	cases := []struct {
		input    string
		expected string
		matched  bool
	}{
		{"BAL01", "01", true},
		{"bal03", "03", true},
		{"BAL10", "10", true},
		{"BAL92", "92", true},
		{"BALANCAS", "", false},
		{"blackbox.zip", "", false},
		{"IMPORT", "", false},
		{".snapshot", "", false},
	}

	for _, tc := range cases {
		sub, ok := m.MatchFolder(tc.input)
		assert.Equal(t, tc.matched, ok, "Input: %s", tc.input)
		if tc.matched {
			assert.Equal(t, tc.expected, sub, "Input: %s", tc.input)
		}
	}
}

func TestFolderMapper_GetLocalFolder(t *testing.T) {
	m, err := mapper.NewFolderMapper(`(?i)^bal(\d+)$`)
	require.NoError(t, err)

	baseDir := `C:\balancas`
	target, ok := m.GetLocalFolder(baseDir, "BAL01")
	assert.True(t, ok)
	assert.Equal(t, filepath.Join(baseDir, "01"), target)

	_, ok = m.GetLocalFolder(baseDir, "BALANCAS")
	assert.False(t, ok)
}

func TestFolderMapper_DefaultPattern(t *testing.T) {
	m, err := mapper.NewFolderMapper("")
	require.NoError(t, err)
	require.NotNil(t, m)

	sub, ok := m.MatchFolder("BAL05")
	assert.True(t, ok)
	assert.Equal(t, "05", sub)
}

func TestFolderMapper_InvalidPattern(t *testing.T) {
	m, err := mapper.NewFolderMapper(`(?i)[unclosed`)
	assert.Error(t, err)
	assert.Nil(t, m)
}

func TestFolderMapper_PatternWithoutCaptureGroup(t *testing.T) {
	m, err := mapper.NewFolderMapper(`^bal\d+$`)
	require.NoError(t, err)
	require.NotNil(t, m)

	sub, ok := m.MatchFolder("bal05")
	assert.True(t, ok)
	assert.Equal(t, "bal05", sub)
}
