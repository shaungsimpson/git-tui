# Releasing

Part of the [development guide](README.md).

A release is a git tag on `main` plus a GitHub Release with notes; there is no version file to bump. Release once the pull requests for it are merged and CI has passed on `main`.

1. **Pick the version** and keep it in a shell variable; the commands below use it. Below 1.0, a new feature bumps the middle number (`v0.4.0` to `v0.5.0`); fixes alone bump the last (`v0.4.1`).

   ```sh
   v=v0.4.2
   ```

2. **Check `main`** is clean, exactly `origin/main`, and green. The commands are chained, so the first failure stops the rest; then confirm the CI run listed for this exact commit passed:

   ```sh
   git switch main &&
   git pull --ff-only &&
   test -z "$(git status --porcelain)" &&
   test "$(git rev-parse HEAD)" = "$(git rev-parse origin/main)" &&
   mise run lint &&
   mise run test &&
   gh run list --commit "$(git rev-parse HEAD)"
   ```

3. **Tag it and push the tag.** The tag's message is a short summary, so `git tag -n1` reads as a one-line changelog and `git show "$v"` gives the gist offline; the full notes go in the GitHub Release (step 4). Write it in a file outside the clone, such as `/tmp/tag.txt`: the version and a title of a few words, a blank line, then up to three lines on what changed and anything users must know. For example:

   ```text
   v0.15.0: Repository groups

   - Give a repository a group with e in Connections, or "group" in config.json
   - ] and [ show one group at a time; hidden repositories make no git or gh calls
   - Configs using group need v0.15.0 or later
   ```

   Then tag with it and push the tag:

   ```sh
   git tag -a "$v" -F /tmp/tag.txt && git push origin "$v"
   ```

   A pushed tag is never moved or rewritten: Go's module cache and checksum database remember the commit each version points to. v0.15.0 and earlier keep their old messages.

4. **Write the notes** in a file outside the clone, such as `/tmp/notes.md`, in the same shape as earlier releases (see [v0.4.1](https://github.com/shaungsimpson/git-tui/releases/tag/v0.4.1)): **New**, **Changes**, **Fixes**, **Notes** for anything users should know (such as extra `gh` calls), **Install or upgrade**, and the full changelog link. Leave out sections with nothing in them. Then publish the GitHub Release:

   ```sh
   gh release create "$v" --title "$v" --notes-file /tmp/notes.md --latest --verify-tag
   ```

   Installed releases learn about the new one from the GitHub Release marked latest, within a day; a tag alone is not announced.

5. **Check it installs** like a user would, from outside the clone. The check prints `installed` only when the `git-tui` first on your PATH is the new version:

   ```sh
   GOPRIVATE=github.com/acme go install "github.com/shaungsimpson/git-tui/cmd/git-tui@$v" &&
   test "$(git-tui -version)" = "git-tui $v" && echo installed
   ```

   `@latest` can take a few minutes to see a new tag; the exact version works at once.
