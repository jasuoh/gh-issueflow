package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func snippet(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "lazygit.yml"))
	require.NoError(t, err)
	return data
}

// commands decodes the custom commands of a config into key -> description.
func commands(t *testing.T, config []byte) []map[string]any {
	t.Helper()
	var parsed struct {
		CustomCommands []map[string]any `yaml:"customCommands"`
	}
	require.NoError(t, yaml.Unmarshal(config, &parsed))
	return parsed.CustomCommands
}

const otherCommand = `
gui:
  # my favourite theme
  theme:
    activeBorderColor: [green, bold]
customCommands:
  - key: C
    context: files
    command: git commit --no-verify
    description: Commit without hooks
`

const oldMenu = `
customCommands:
  - key: I
    description: GitHub issues (old)
    commandMenu:
      - key: i
        context: global
        command: gh issue develop {{.Form.Issue}} --checkout
  - key: C
    context: files
    command: git commit --no-verify
`

func TestInstallIntoEmptyConfig(t *testing.T) {
	out, result, err := Install(nil, snippet(t), "I", false)

	require.NoError(t, err)
	assert.Equal(t, Added, result)
	cmds := commands(t, out)
	require.Len(t, cmds, 1)
	assert.Equal(t, "I", cmds[0]["key"])
	assert.Contains(t, string(out), "gh issueflow pick {{ .Form.Issue }}")
}

func TestInstallKeepsTheRestOfTheConfig(t *testing.T) {
	out, result, err := Install([]byte(otherCommand), snippet(t), "I", false)

	require.NoError(t, err)
	assert.Equal(t, Added, result)
	assert.Contains(t, string(out), "# my favourite theme")
	assert.Contains(t, string(out), "activeBorderColor")
	cmds := commands(t, out)
	require.Len(t, cmds, 2)
	assert.Equal(t, "C", cmds[0]["key"])
	assert.Equal(t, "I", cmds[1]["key"])
}

func TestInstallTwiceChangesNothing(t *testing.T) {
	first, _, err := Install([]byte(otherCommand), snippet(t), "I", false)
	require.NoError(t, err)

	second, result, err := Install(first, snippet(t), "I", false)

	require.NoError(t, err)
	assert.Equal(t, Unchanged, result)
	assert.Equal(t, string(first), string(second))
}

func TestInstallRefusesToShadowAnotherCommand(t *testing.T) {
	_, _, err := Install([]byte(oldMenu), snippet(t), "I", false)

	var keyTaken *KeyTakenError
	require.ErrorAs(t, err, &keyTaken)
	assert.Equal(t, "the custom command 'GitHub issues (old)' already uses the key I in your lazygit config. Use --replace to replace it, or --key to pick another key", err.Error())
}

func TestInstallReplacesAnotherCommandWhenAsked(t *testing.T) {
	out, result, err := Install([]byte(oldMenu), snippet(t), "I", true)

	require.NoError(t, err)
	assert.Equal(t, Updated, result)
	assert.NotContains(t, string(out), "GitHub issues (old)")
	cmds := commands(t, out)
	require.Len(t, cmds, 2)
	// The menu takes the place of the command it replaces.
	assert.Equal(t, "I", cmds[0]["key"])
	assert.Equal(t, "GitHub issues", cmds[0]["description"])
	assert.Equal(t, "C", cmds[1]["key"])
}

func TestInstallMovesTheMenuToAnotherKey(t *testing.T) {
	installed, _, err := Install([]byte(otherCommand), snippet(t), "I", false)
	require.NoError(t, err)

	out, result, err := Install(installed, snippet(t), "<c-i>", false)

	require.NoError(t, err)
	assert.Equal(t, Updated, result)
	cmds := commands(t, out)
	require.Len(t, cmds, 2)
	assert.Equal(t, "<c-i>", cmds[1]["key"])
}

func TestRemove(t *testing.T) {
	installed, _, err := Install([]byte(otherCommand), snippet(t), "I", false)
	require.NoError(t, err)

	out, result, err := Remove(installed)

	require.NoError(t, err)
	assert.Equal(t, Removed, result)
	assert.NotContains(t, string(out), "gh issueflow")
	assert.Contains(t, string(out), "# my favourite theme")
	require.Len(t, commands(t, out), 1)

	_, result, err = Remove(out)
	require.NoError(t, err)
	assert.Equal(t, NotFound, result)
}

func TestWriteWithBackupKeepsTheOriginal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	require.NoError(t, os.WriteFile(path, []byte("original\n"), 0o600))

	backup, err := WriteWithBackup(path, []byte("first\n"))
	require.NoError(t, err)
	_, err = WriteWithBackup(path, []byte("second\n"))
	require.NoError(t, err)

	assert.Equal(t, path+".before-issueflow", backup)
	saved, _ := os.ReadFile(backup)
	assert.Equal(t, "original\n", string(saved))
	current, _ := os.ReadFile(path)
	assert.Equal(t, "second\n", string(current))
	info, _ := os.Stat(path)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestWriteWithBackupCreatesAMissingConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lazygit", "config.yml")

	backup, err := WriteWithBackup(path, []byte("new\n"))

	require.NoError(t, err)
	assert.Equal(t, "", backup)
	current, _ := os.ReadFile(path)
	assert.True(t, strings.HasPrefix(string(current), "new"))
}
