package flow

import (
	"fmt"
	"strconv"

	"github.com/jasuoh/gh-issueflow/internal/git"
)

// Config is read from git config, so it can be set globally or per
// repository:
//
//	git config --global issueflow.projectNumber 3
type Config struct {
	// The user or organization that owns the project; the repository owner
	// if empty.
	ProjectOwner string
	// 0 disables all project features.
	ProjectNumber int
	// The project's single-select field holding an item's status.
	StatusField string
	InProgress  string
	InReview    string
	Done        string
}

func LoadConfig(repo *git.Repo) (Config, error) {
	get := func(key string, fallback string) string {
		if value := repo.Config("issueflow." + key); value != "" {
			return value
		}
		return fallback
	}

	config := Config{
		ProjectOwner: get("projectOwner", ""),
		StatusField:  get("statusField", "Status"),
		InProgress:   get("statusInProgress", "In Progress"),
		InReview:     get("statusInReview", "Needs review"),
		Done:         get("statusDone", "Done"),
	}

	if number := get("projectNumber", ""); number != "" {
		n, err := strconv.Atoi(number)
		if err != nil || n < 0 {
			return Config{}, fmt.Errorf("issueflow.projectNumber must be a number, got '%s'", number)
		}
		config.ProjectNumber = n
	}
	return config, nil
}
