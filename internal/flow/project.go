package flow

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jasuoh/gh-issueflow/internal/github"
)

// ProjectCache remembers the IDs of project boards across invocations, since
// every subcommand is a process of its own and looking a board up takes a
// request. An entry is refreshed when a status the user asks for isn't in
// it, which is how renamed or added columns show up.
type ProjectCache struct {
	Path string
}

func DefaultProjectCache() *ProjectCache {
	dir, err := os.UserCacheDir()
	if err != nil {
		return &ProjectCache{}
	}
	return &ProjectCache{Path: filepath.Join(dir, "gh-issueflow", "projects.json")}
}

func (self *ProjectCache) load() map[string]*github.Project {
	entries := map[string]*github.Project{}
	if self.Path == "" {
		return entries
	}
	if data, err := os.ReadFile(self.Path); err == nil {
		_ = json.Unmarshal(data, &entries)
	}
	return entries
}

func (self *ProjectCache) get(key string) *github.Project {
	return self.load()[key]
}

// put stores the entry. A cache that can't be written only costs a request
// next time, so failures are ignored.
func (self *ProjectCache) put(key string, project *github.Project) {
	if self.Path == "" {
		return
	}
	entries := self.load()
	entries[key] = project
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return
	}
	if os.MkdirAll(filepath.Dir(self.Path), 0o755) != nil {
		return
	}
	tmp := self.Path + ".tmp"
	if os.WriteFile(tmp, data, 0o600) == nil {
		_ = os.Rename(tmp, self.Path)
	}
}

func (self *App) projectCacheKey() string {
	owner := self.Config.ProjectOwner
	if owner == "" {
		owner = self.Client.Owner()
	}
	return strings.Join([]string{self.Host, strings.ToLower(owner), strconv.Itoa(self.Config.ProjectNumber), self.Config.StatusField}, "|")
}

func (self *App) fetchProject() (*github.Project, error) {
	owner := self.Config.ProjectOwner
	if owner == "" {
		owner = self.Client.Owner()
	}
	project, err := self.Client.Project(owner, self.Config.ProjectNumber, self.Config.StatusField)
	if err != nil {
		return nil, err
	}
	self.Cache.put(self.projectCacheKey(), project)
	return project, nil
}

// project returns the configured board, or nil if projects are disabled.
func (self *App) project() (*github.Project, error) {
	if self.Config.ProjectNumber == 0 {
		return nil, nil
	}
	if cached := self.Cache.get(self.projectCacheKey()); cached != nil {
		return cached, nil
	}
	return self.fetchProject()
}

func (self *App) requireProject() (*github.Project, error) {
	project, err := self.project()
	if err == nil && project == nil {
		return nil, errors.New("no project board configured. Run: git config --global issueflow.projectNumber <number>")
	}
	return project, err
}

// board is a status to set on the project board; nil when projects are
// disabled, in which case setting it does nothing.
type board struct {
	project  *github.Project
	optionID string
}

func (self *App) board(status string) (*board, error) {
	project, err := self.project()
	if err != nil || project == nil {
		return nil, err
	}

	optionID, ok := project.StatusOptionID(status)
	if !ok {
		// The cached board may predate a renamed or added column.
		if project, err = self.fetchProject(); err != nil {
			return nil, err
		}
		if optionID, ok = project.StatusOptionID(status); !ok {
			return nil, fmt.Errorf("the project field '%s' has no option named '%s'", self.Config.StatusField, status)
		}
	}
	return &board{project: project, optionID: optionID}, nil
}

// setStatus puts the issue (or pull request) with the given node ID on the
// board, if it isn't there already, and sets its status.
func (self *board) setStatus(client *github.Client, contentID string) error {
	if self == nil {
		return nil
	}
	itemID, err := client.AddToProject(self.project.ID, contentID)
	if err != nil {
		return err
	}
	return client.SetProjectItemStatus(self.project, itemID, self.optionID)
}
