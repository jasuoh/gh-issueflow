# gh-issueflow

[![CI](https://github.com/jasuoh/gh-issueflow/actions/workflows/ci.yml/badge.svg)](https://github.com/jasuoh/gh-issueflow/actions/workflows/ci.yml)

Work on GitHub issues without leaving [lazygit](https://github.com/jesseduffield/lazygit): pick an issue, get a branch for it, open the pull request, and merge it, while your GitHub project board keeps up by itself.

```
 pick issue ─────────▶ open pull request ──────▶ merge
 assigned to you       pushed, "Closes #12"      squash-merged, branch deleted
 branch 12-fix-login   status: Needs review      status: Done (if someone else approved)
 status: In Progress
```

It's a [GitHub CLI](https://cli.github.com/) extension, so it uses your existing `gh` login, and lazygit drives it through its regular custom commands; no lazygit fork or patch involved.

## Quick start

```sh
gh extension install jasuoh/gh-issueflow
gh issueflow setup
```

Then press `I` in lazygit.

`setup` adds the menu to your lazygit config and keeps a backup of the config as it was (`config.yml.before-issueflow`). Run `gh issueflow setup --dry-run` first to see the result without changing anything.

**With a project board**, also pass its number (from its URL, e.g. `https://github.com/users/me/projects/3`) and give gh access to projects:

```sh
gh issueflow setup --project-number 3
gh auth refresh -s project
```

You need [gh](https://cli.github.com/) (logged in with `gh auth login`) and lazygit. Go isn't needed: `gh extension install` downloads a ready-made binary.

## The menu

Press `I` anywhere in lazygit:

| Key | Action | What happens |
| --- | --- | --- |
| `p` | Pick issue | Choose from the open issues (type to filter). It's assigned to you, a branch `<number>-<title>` is created on GitHub, linked to the issue (it shows in the issue's *Development* section), and checked out. Status → *In Progress*. |
| `n` | New issue | Create an issue assigned to you and put it on the board. |
| `c` | Create pull request | Push the current branch and open a pull request against the default branch that says `Closes #<number>`, taken from the digits the branch name starts with. Status → *Needs review*. |
| `s` | Set status | Pick any column of your board for the current branch's issue. |
| `m` | Merge pull request | After confirming: squash-merge the current branch's pull request, delete the branch on GitHub and locally, and switch to the default branch. Status → *Done*, **but only if someone other than you approved the pull request**; otherwise the status stays and you're told why. |
| `b` | Delete branch | Pick a local branch and delete it locally and on GitHub. No error if GitHub deleted it already. |
| `D` | Delete issue | Permanently delete an issue, after an explicit warning (needs admin rights). |

The pull request title is the commit message if there's a single commit, otherwise the issue's title.

## Project boards

Everything works without a board; with one, issues move across it as described above. Settings live in git config, so they can be global or per repository (leave out `--global`):

```sh
git config --global issueflow.projectNumber 3
```

| Setting | Default | Meaning |
| --- | --- | --- |
| `issueflow.projectNumber` | unset | The board's number; unset or `0` turns the board features off |
| `issueflow.projectOwner` | repository owner | User or organization that owns the board |
| `issueflow.statusField` | `Status` | The board's single-select status field |
| `issueflow.statusInProgress` | `In Progress` | Set when picking an issue |
| `issueflow.statusInReview` | `Needs review` | Set when opening the pull request |
| `issueflow.statusDone` | `Done` | Set when merging an approved pull request |

The status names must match columns of your board (case doesn't matter). The board's IDs are cached and refreshed automatically when you use a column the cache doesn't know yet.

## Using it without lazygit

Every menu entry is a plain command, run inside a repository:

```
gh issueflow issues                  list open issues as "<number><TAB><title>"
gh issueflow pick <number>
gh issueflow new <title>
gh issueflow pr
gh issueflow statuses                list the board's columns
gh issueflow status <status>
gh issueflow merge
gh issueflow branches                list local branches except the checked-out one
gh issueflow delete-branch <name>
gh issueflow delete-issue <number>
```

The repository is chosen the way gh chooses it, including `gh repo set-default`.

## Security

- Your token stays with gh: requests go through gh's own HTTP client, which only sends it to the GitHub host you logged into.
- gh-issueflow starts git directly rather than through a shell, and the lazygit menu only hands it numbers and quoted values. Text from GitHub, like issue titles anyone can write, is stripped of control characters before it's shown, so it can't hijack your terminal or forge menu entries.
- Release binaries are built by GitHub Actions with signed provenance; check yours with `gh attestation verify ~/.local/share/gh/extensions/gh-issueflow/gh-issueflow --repo jasuoh/gh-issueflow`.

Details, and how to report a vulnerability, are in [SECURITY.md](SECURITY.md).

## Updating and uninstalling

```sh
gh extension upgrade issueflow   # update; run `gh issueflow setup` afterwards to pick up menu changes
gh issueflow setup --remove      # take the menu out of your lazygit config
gh extension remove issueflow    # uninstall
```

## Troubleshooting

| Message | Fix |
| --- | --- |
| `… already uses the key I in your lazygit config` | Another custom command is on `I`. Replace it with `gh issueflow setup --replace`, or choose another key with `--key`, e.g. `--key X`. |
| `… lacks the 'project' scope …` | `gh auth refresh -s project` |
| `no project board configured` | Only *Set status* needs a board: `git config --global issueflow.projectNumber <n>` |
| `the project field 'Status' has no option named …` | Make the `issueflow.status*` settings match your board's column names. |
| `isn't named after an issue` | *Set status* and the `Closes #n` line work with branches whose names start with the issue number, as picked branches do. |

## Development

```sh
go test ./...
```

The tests run every command against a fake GitHub API and real temporary git repositories, without network access. Releases are made by pushing a `v*` tag.

## License

MIT; see [LICENSE](LICENSE). Parts of the code come from lazygit's GitHub integration.
