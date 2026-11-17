// Package setup adds the gh-issueflow menu to a lazygit config file.
package setup

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"gopkg.in/yaml.v3"
)

// Result tells what Install or Remove did to the config.
type Result int

const (
	Added Result = iota
	Updated
	Unchanged
	Removed
	NotFound
)

// KeyTakenError is returned when another custom command already uses the key
// that the menu is meant to go on.
type KeyTakenError struct {
	Key         string
	Description string
}

func (self *KeyTakenError) Error() string {
	what := "another custom command"
	if self.Description != "" {
		what = fmt.Sprintf("the custom command '%s'", self.Description)
	}
	return fmt.Sprintf("%s already uses the key %s in your lazygit config. Use --replace to replace it, or --key to pick another key", what, self.Key)
}

// Install puts the menu from snippet (a lazygit config whose customCommands
// holds the menu as its only entry) on the given key in config, and returns
// the new config. An existing gh-issueflow menu is updated in place, even if
// it is on another key; any other command on the key is only replaced if
// replace is set.
func Install(config []byte, snippet []byte, key string, replace bool) ([]byte, Result, error) {
	menu, err := menuFromSnippet(snippet)
	if err != nil {
		return nil, 0, err
	}
	setMapValue(menu, "key", key)

	doc, err := parseConfig(config)
	if err != nil {
		return nil, 0, err
	}
	commands := customCommands(doc, true)

	oursAt := -1
	var conflicts []*yaml.Node
	for i, item := range commands.Content {
		switch {
		case isOurs(item):
			if oursAt == -1 {
				oursAt = i
			}
		case mapValue(item, "key") == key:
			conflicts = append(conflicts, item)
		}
	}
	if len(conflicts) > 0 && !replace {
		return nil, 0, &KeyTakenError{Key: key, Description: mapValue(conflicts[0], "description")}
	}
	if oursAt != -1 && len(conflicts) == 0 && encodeNode(commands.Content[oursAt]) == encodeNode(menu) {
		return config, Unchanged, nil
	}

	// The menu takes the place of the existing one, or else of the command it
	// replaces; other commands on the key would shadow it, and are dropped.
	insertAt := oursAt
	if insertAt == -1 && len(conflicts) > 0 {
		insertAt = indexOf(commands.Content, conflicts[0])
	}
	var items []*yaml.Node
	for i, item := range commands.Content {
		if i == insertAt {
			items = append(items, menu)
		} else if !isOurs(item) && mapValue(item, "key") != key {
			items = append(items, item)
		}
	}
	result := Updated
	if insertAt == -1 {
		items = append(items, menu)
		result = Added
	}
	commands.Content = items

	out, err := encodeDoc(doc)
	return out, result, err
}

// Remove takes the gh-issueflow menu out of config.
func Remove(config []byte) ([]byte, Result, error) {
	doc, err := parseConfig(config)
	if err != nil {
		return nil, 0, err
	}
	commands := customCommands(doc, false)
	if commands == nil {
		return config, NotFound, nil
	}

	kept := filterNodes(commands.Content, func(item *yaml.Node) bool { return !isOurs(item) })
	if len(kept) == len(commands.Content) {
		return config, NotFound, nil
	}
	commands.Content = kept
	out, err := encodeDoc(doc)
	return out, Removed, err
}

func indexOf(nodes []*yaml.Node, node *yaml.Node) int {
	for i, n := range nodes {
		if n == node {
			return i
		}
	}
	return -1
}

func filterNodes(nodes []*yaml.Node, keep func(*yaml.Node) bool) []*yaml.Node {
	result := make([]*yaml.Node, 0, len(nodes))
	for _, node := range nodes {
		if keep(node) {
			result = append(result, node)
		}
	}
	return result
}

func menuFromSnippet(snippet []byte) (*yaml.Node, error) {
	doc, err := parseConfig(snippet)
	if err != nil {
		return nil, err
	}
	commands := customCommands(doc, false)
	if commands == nil || len(commands.Content) != 1 {
		return nil, errors.New("the bundled lazygit snippet must hold exactly one custom command")
	}
	return commands.Content[0], nil
}

// parseConfig parses a lazygit config, treating an empty file as an empty
// mapping.
func parseConfig(config []byte) (*yaml.Node, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(config, &doc); err != nil {
		return nil, fmt.Errorf("can't parse the lazygit config: %w", err)
	}
	if doc.Kind == 0 {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}}
	}
	if len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("the lazygit config must be a YAML mapping")
	}
	return &doc, nil
}

// customCommands returns the customCommands sequence of the config, creating
// it if create is set, or nil.
func customCommands(doc *yaml.Node, create bool) *yaml.Node {
	root := doc.Content[0]
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != "customCommands" {
			continue
		}
		value := root.Content[i+1]
		if value.Kind != yaml.SequenceNode {
			if !create {
				return nil
			}
			*value = yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		}
		return value
	}
	if !create {
		return nil
	}
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "customCommands"}, seq)
	return seq
}

func mapValue(node *yaml.Node, key string) string {
	if node.Kind != yaml.MappingNode {
		return ""
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1].Value
		}
	}
	return ""
}

func setMapValue(node *yaml.Node, key string, value string) {
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			node.Content[i+1].Value = value
			node.Content[i+1].Style = yaml.DoubleQuotedStyle
			return
		}
	}
}

// isOurs tells whether a custom command is the gh-issueflow menu, i.e. one
// whose commands run gh issueflow.
func isOurs(node *yaml.Node) bool {
	if node.Kind == yaml.ScalarNode {
		return strings.Contains(node.Value, "gh issueflow")
	}
	for _, child := range node.Content {
		if isOurs(child) {
			return true
		}
	}
	return false
}

func encodeNode(node *yaml.Node) string {
	out, _ := yaml.Marshal(node)
	return string(out)
}

func encodeDoc(doc *yaml.Node) ([]byte, error) {
	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(doc); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ConfigPath finds the lazygit config file the way lazygit does.
func ConfigPath() string {
	if files := os.Getenv("LG_CONFIG_FILE"); files != "" {
		return strings.Split(files, ",")[0]
	}
	if dir := os.Getenv("CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "config.yml")
	}
	if output, err := exec.Command("lazygit", "--print-config-dir").Output(); err == nil {
		if dir := strings.TrimSpace(string(output)); dir != "" {
			return filepath.Join(dir, "config.yml")
		}
	}
	return filepath.Join(defaultConfigHome(), "lazygit", "config.yml")
}

// defaultConfigHome mirrors the XDG config home that lazygit uses when the
// lazygit binary can't be asked.
func defaultConfigHome() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support")
	case "windows":
		if dir := os.Getenv("LOCALAPPDATA"); dir != "" {
			return dir
		}
	}
	return filepath.Join(home, ".config")
}

// WriteWithBackup writes the config, keeping the file as it was before
// gh-issueflow first touched it next to it.
func WriteWithBackup(path string, contents []byte) (backup string, err error) {
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
		backup = path + ".before-issueflow"
		if _, err := os.Stat(backup); errors.Is(err, os.ErrNotExist) {
			original, err := os.ReadFile(path)
			if err != nil {
				return "", err
			}
			if err := os.WriteFile(backup, original, mode); err != nil {
				return "", err
			}
		}
	} else if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, contents, mode); err != nil {
		return "", err
	}
	return backup, os.Rename(tmp, path)
}
