# gh-issueflow

A [GitHub CLI](https://cli.github.com/) extension that takes a GitHub issue from "I'll do this" to merged, and keeps your GitHub project (v2) board up to date on the way. It's made to be driven from [lazygit](https://github.com/jesseduffield/lazygit), but works on its own too.

| Command | What it does |
| --- | --- |
| `gh issueflow issues` | List open issues as `<number>\t<title>` |
| `gh issueflow pick <n>` | Assign the issue to yourself, create a branch `<n>-<slugified-title>` linked to it on GitHub (it shows up in the issue's Development section), check it out, and set the board status to *In Progress* |
| `gh issueflow new <title>` | Create an issue assigned to yourself and add it to the board |
| `gh issueflow pr` | Push the current branch and open a pull request against the default branch with `Closes #<n>` (`n` = the digits the branch name starts with); set the status to *Needs review* |
| `gh issueflow statuses` | List the board's status options |
| `gh issueflow status <status>` | Set the status of the current branch's issue |
| `gh issueflow merge` | Squash-merge the current branch's pull request, delete the branch on the remote and locally, and switch to the default branch. The issue is set to *Done* **only if someone other than you approved** the pull request |
| `gh issueflow branches` | List local branches other than the checked-out one |
| `gh issueflow delete-branch <name>` | Delete a branch locally and on its remote (no error if the remote branch is gone already) |
| `gh issueflow delete-issue <n>` | Permanently delete an issue (needs admin rights) |

## Install

```sh
gh extension install jasuoh/gh-issueflow
```

It uses gh's login (`gh auth login`) and picks the repository the way gh does, including `gh repo set-default`. The board features additionally need the `project` scope:

```sh
gh auth refresh -s project
```

## Configure

Settings live in git config, so you can set them globally or per repository:

```sh
git config --global issueflow.projectNumber 3   # from the project's URL; unset/0 disables the board features
git config --global issueflow.projectOwner my-org   # default: the repository owner
```

| Key | Default |
| --- | --- |
| `issueflow.projectNumber` | unset (board features disabled; the issue features still work) |
| `issueflow.projectOwner` | the repository owner |
| `issueflow.statusField` | `Status` |
| `issueflow.statusInProgress` | `In Progress` |
| `issueflow.statusInReview` | `Needs review` |
| `issueflow.statusDone` | `Done` |

The board's IDs are cached in your user cache directory, and refreshed automatically when you use a status the cache doesn't know yet.

## Use from lazygit

Copy [`lazygit.yml`](lazygit.yml) into your lazygit config. Pressing `I` then opens a menu with all the commands above, with filterable issue and status lists, loading indicators, and confirmations for merging and deleting.

## Develop

```sh
go test ./...
```

The tests run every command against a fake GitHub GraphQL API and real temporary git repositories; they don't need network access.
