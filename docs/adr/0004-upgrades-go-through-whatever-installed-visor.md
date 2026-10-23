# Upgrades go through whatever installed visor

visor checks for a new release and offers an Upgrade, but it never replaces its own binary. It works out how it was installed from where its executable lives and hands the Upgrade to that: it runs `brew upgrade --cask visor` for a Homebrew install, and for a system package, `go install` or a downloaded archive it shows the command to run or opens the release page.

## Considered Options

- **Self-replace (lazygit, `go-selfupdate`)**: one path for every install, but it fights the package manager. Homebrew's policy is that software which updates itself "conflicts with Homebrew's version and upgrade management", and a cask's binary sits in a versioned `Caskroom` folder that a swap would leave misnamed. It also needs checksum and signature checks to be safe, and the cask ships two binaries (`visor`, `visor-weather`).
- **Notify only (gh, k9s, Atuin)**: the safest, but it leaves the Homebrew user, the common case, to leave visor and type the command.

## Consequences

- apt, dnf and apk installs only get the command, since they need root.
- Self-replace stays possible later for archive installs alone, behind a verified `checksums.txt`.
