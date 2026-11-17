// gh-issueflow takes a GitHub issue from "I'll do this" to merged: pick it,
// branch off it, open a pull request for it, and merge it, while keeping a
// GitHub project (v2) board up to date. It is built to be driven from
// lazygit's custom commands, but works on its own too.
package main

import (
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strconv"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/jasuoh/gh-issueflow/internal/flow"
	"github.com/jasuoh/gh-issueflow/internal/git"
	"github.com/jasuoh/gh-issueflow/internal/github"
	"github.com/jasuoh/gh-issueflow/internal/setup"
)

// The custom commands that drive gh-issueflow from lazygit.
//
//go:embed lazygit.yml
var lazygitSnippet []byte

const usage = `Usage: gh issueflow <command> [args]

Setup:
  setup [flags]        Add the GitHub issues menu to your lazygit config
                       (a backup of the config is kept next to it)
    --key <key>          key for the menu (default: I)
    --replace            replace another custom command on that key
    --project-number <n> also set issueflow.projectNumber globally
    --project-owner <o>  also set issueflow.projectOwner globally
    --config <path>      lazygit config file (default: the one lazygit uses)
    --dry-run            print the new config instead of writing it
    --remove             remove the menu again

Commands:
  issues               List open issues as "<number>\t<title>"
  pick <number>        Assign the issue to yourself, create a linked branch for
                       it and check it out, and set its status to in progress
  new <title>          Create an issue assigned to yourself, on the board
  pr                   Push the current branch and open a pull request that
                       closes its issue; set the issue's status to in review
  statuses             List the status options of the project board
  status <status>      Set the status of the current branch's issue
  merge                Squash-merge the current branch's pull request, delete
                       the branch locally and remotely, and switch to the
                       default branch; the issue is set to done if someone
                       other than you approved the pull request
  branches             List local branches other than the checked-out one
  delete-branch <name> Delete a branch locally and on its remote
  delete-issue <number>
                       Permanently delete an issue

Configuration (git config, e.g. with --global):
  issueflow.projectNumber     project number from its URL; unset or 0 disables
                              the project board features
  issueflow.projectOwner      user or org owning the project (default: the
                              repository owner)
  issueflow.statusField       name of the status field (default: Status)
  issueflow.statusInProgress  default: In Progress
  issueflow.statusInReview    default: Needs review
  issueflow.statusDone        default: Done
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "gh issueflow: "+flow.SanitizeMultiline(flow.TranslateError(err).Error()))
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		fmt.Print(usage)
		return nil
	}
	command, args := args[0], args[1:]
	if command == "setup" {
		return runSetup(args)
	}

	wantArgs := map[string]int{
		"issues": 0, "pick": 1, "new": 1, "pr": 0, "statuses": 0, "status": 1,
		"merge": 0, "branches": 0, "delete-branch": 1, "delete-issue": 1,
	}
	n, known := wantArgs[command]
	if !known {
		return fmt.Errorf("unknown command '%s'; run 'gh issueflow help'", command)
	}
	if len(args) != n {
		return fmt.Errorf("'%s' takes %d argument(s); run 'gh issueflow help'", command, n)
	}

	app, err := newApp()
	if err != nil {
		return err
	}

	switch command {
	case "issues":
		return app.Issues()
	case "pick":
		number, err := flow.ParseIssueNumber(args[0])
		if err != nil {
			return err
		}
		return app.Pick(number)
	case "new":
		return app.New(args[0])
	case "pr":
		return app.PullRequest()
	case "statuses":
		return app.Statuses()
	case "status":
		return app.SetStatus(args[0])
	case "merge":
		return app.Merge()
	case "branches":
		return app.Branches()
	case "delete-branch":
		return app.DeleteBranch(args[0])
	default: // "delete-issue"
		number, err := flow.ParseIssueNumber(args[0])
		if err != nil {
			return err
		}
		return app.DeleteIssue(number)
	}
}

func newApp() (*flow.App, error) {
	// Resolves the repository the way gh does: GH_REPO, then the remote
	// marked by `gh repo set-default`, then the conventional remotes.
	repo, err := repository.Current()
	if err != nil {
		return nil, err
	}

	gitRepo := &git.Repo{}
	remoteURLs, err := gitRepo.RemoteURLs()
	if err != nil {
		return nil, err
	}
	remote := flow.FindRemote(remoteURLs, repo.Owner, repo.Name)
	if remote == "" {
		return nil, fmt.Errorf("no git remote points at %s/%s", repo.Owner, repo.Name)
	}

	config, err := flow.LoadConfig(gitRepo)
	if err != nil {
		return nil, err
	}

	httpClient, err := api.NewHTTPClient(api.ClientOptions{Host: repo.Host})
	if err != nil {
		return nil, errors.New("not logged in to GitHub. Run: gh auth login")
	}

	return &flow.App{
		Git:    gitRepo,
		Client: github.NewClient(github.Endpoint(repo.Host), httpClient, repo.Owner, repo.Name),
		Host:   repo.Host,
		Remote: remote,
		Config: config,
		Cache:  flow.DefaultProjectCache(),
		Out:    os.Stdout,
	}, nil
}

func runSetup(args []string) error {
	flags := flag.NewFlagSet("setup", flag.ContinueOnError)
	key := flags.String("key", "I", "")
	replace := flags.Bool("replace", false, "")
	projectNumber := flags.Int("project-number", -1, "")
	projectOwner := flags.String("project-owner", "", "")
	configPath := flags.String("config", "", "")
	dryRun := flags.Bool("dry-run", false, "")
	remove := flags.Bool("remove", false, "")
	flags.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return fmt.Errorf("unexpected argument '%s'", flags.Arg(0))
	}

	path := *configPath
	if path == "" {
		path = setup.ConfigPath()
	}
	current, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	var updated []byte
	var result setup.Result
	if *remove {
		updated, result, err = setup.Remove(current)
	} else {
		updated, result, err = setup.Install(current, lazygitSnippet, *key, *replace)
	}
	if err != nil {
		return err
	}

	if *dryRun {
		fmt.Print(string(updated))
		return nil
	}

	switch result {
	case setup.Unchanged:
		fmt.Printf("The menu is already up to date in %s\n", path)
	case setup.NotFound:
		fmt.Printf("There is no gh-issueflow menu in %s\n", path)
	default:
		backup, err := setup.WriteWithBackup(path, updated)
		if err != nil {
			return err
		}
		verb := map[setup.Result]string{setup.Added: "Added the menu to", setup.Updated: "Updated the menu in", setup.Removed: "Removed the menu from"}[result]
		fmt.Printf("%s %s\n", verb, path)
		if backup != "" {
			fmt.Printf("The config as it was before gh-issueflow changed it is in %s\n", backup)
		}
	}

	if *remove {
		return nil
	}
	if *projectNumber >= 0 {
		if err := setGlobalGitConfig("issueflow.projectNumber", strconv.Itoa(*projectNumber)); err != nil {
			return err
		}
		fmt.Printf("Set issueflow.projectNumber to %d\n", *projectNumber)
	}
	if *projectOwner != "" {
		if err := setGlobalGitConfig("issueflow.projectOwner", *projectOwner); err != nil {
			return err
		}
		fmt.Printf("Set issueflow.projectOwner to %s\n", *projectOwner)
	}

	fmt.Printf("\nPress %s in lazygit to open the menu.\n", *key)
	if *projectNumber > 0 {
		fmt.Println("Project boards need the 'project' scope; if you haven't granted it yet, run: gh auth refresh -s project")
	}
	return nil
}

func setGlobalGitConfig(key string, value string) error {
	output, err := exec.Command("git", "config", "--global", key, value).CombinedOutput()
	if err != nil {
		return fmt.Errorf("git config --global %s: %s", key, output)
	}
	return nil
}
