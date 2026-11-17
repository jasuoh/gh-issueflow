# Security

## Reporting a vulnerability

Please report vulnerabilities privately through [GitHub's private vulnerability reporting](https://github.com/jasuoh/gh-issueflow/security/advisories/new) rather than in a public issue.

## What gh-issueflow does and doesn't do

- **It never sees your token.** Authentication is entirely gh's: requests go through gh's own HTTP client ([go-gh](https://github.com/cli/go-gh)), which only attaches your token to the GitHub host you logged into with `gh auth login`.
- **It runs no shell.** git is started directly with its arguments, never through a shell, so nothing from GitHub is ever interpreted as a command. Branch names it creates consist only of letters, digits and dashes, and it refuses to check out a branch whose name GitHub changed.
- **It treats text from GitHub as untrusted.** Anyone can open an issue on a public repository. Control characters (such as terminal escape sequences, tabs and newlines) and Unicode bidirectional overrides in issue titles and status names are replaced by spaces before they're printed, so they can't take over your terminal or forge entries in lazygit's menus.
- **`setup` only edits your lazygit config**, and keeps a copy of it as it was before (`config.yml.before-issueflow`). `gh issueflow setup --dry-run` shows the result without writing anything; `gh issueflow setup --remove` takes the menu out again. The only program it runs is `lazygit --print-config-dir`, to find the config.
- **There is no `curl | sh` installer.** Install through `gh extension install`, which downloads the binary for your platform from this repository's GitHub releases.

## Verifying a binary

Release binaries are built by GitHub Actions from the tagged source, never on a developer machine, and each one comes with a signed [build provenance attestation](https://docs.github.com/en/actions/security-for-github-actions/using-artifact-attestations). To check that the binary you installed is one of them:

```sh
# Linux and macOS
gh attestation verify ~/.local/share/gh/extensions/gh-issueflow/gh-issueflow --repo jasuoh/gh-issueflow

# Windows (PowerShell)
gh attestation verify "$env:LOCALAPPDATA\GitHub CLI\extensions\gh-issueflow\gh-issueflow.exe" --repo jasuoh/gh-issueflow
```

It succeeds only for a binary that this repository's release workflow built; add `--format json` to see which tag it was built from.

The workflows pin every action to a full commit SHA, grant each job only the permissions it needs, and check dependencies with `govulncheck`; Dependabot proposes updates.
