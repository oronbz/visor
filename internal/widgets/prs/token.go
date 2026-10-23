package prs

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/oronbz/visor/internal/github"
	"github.com/oronbz/visor/internal/ports"
	"github.com/oronbz/visor/internal/widget"
)

// credential names the GitHub token every PRs Widget shares, so a rate limit
// on one pauses them all. It's also the account of visor's keyring item.
const credential = "github:github.com"

const service = "visor"

// scopes are what the search needs: read:org for a Team reviewer's slug.
const scopes = "repo and read:org scopes"

const tokenPage = "https://github.com/settings/tokens/new?scopes=repo,read:org&description=visor"

const pasteToken = "paste-token"

// fromKeyring is how setup hints name the token pasted into visor.
const fromKeyring = "The saved token"

var (
	errNoToken   = errors.New("no GitHub token")
	errNoKeyring = errors.New("no GitHub token and no keyring")
)

// token borrows a GitHub token from GH_TOKEN, GITHUB_TOKEN or `gh auth token`,
// else reads the one pasted into visor from the keyring; from names where it
// came from. It's read afresh each time and never kept, so a `gh auth login`
// is picked up on the next refresh.
func token(ctx context.Context, p ports.Ports) (tok, from string, err error) {
	if tok, from := github.Token(ctx, p.Getenv, p.Commands); tok != "" {
		return tok, from, nil
	}
	if p.Keyring == nil {
		return "", "", errNoKeyring
	}
	t, err := p.Keyring.Get(service, credential)
	switch {
	case errors.Is(err, ports.ErrSecretNotFound):
		return "", "", errNoToken
	case err != nil:
		return "", "", errNoKeyring
	case strings.TrimSpace(t) == "":
		return "", "", errNoToken
	}
	return strings.TrimSpace(t), fromKeyring, nil
}

// fineGrained reports a github_pat_ token, which can't search other orgs' PRs.
func fineGrained(tok string) bool { return strings.HasPrefix(tok, "github_pat_") }

func canStore(p ports.Ports) bool {
	if p.Keyring == nil {
		return false
	}
	_, err := p.Keyring.Get(service, credential)
	return err == nil || errors.Is(err, ports.ErrSecretNotFound)
}

// needsToken is the Status of a Widget with no token to use, err being why.
func needsToken(err error) *widget.SetupError {
	if errors.Is(err, errNoKeyring) {
		return &widget.SetupError{Hint: "Set GH_TOKEN, or run `gh auth login` (no keyring to keep a token in)"}
	}
	return &widget.SetupError{Hint: "Run `gh auth login`, or press Enter to paste a classic token", Action: pasteAction()}
}

// unusable is the Status of a Widget whose token can't be used, problem
// saying why. Only a token pasted into visor can be replaced by pasting
// another: one borrowed from the environment or gh always wins over it.
func unusable(p ports.Ports, from, problem string) *widget.SetupError {
	switch {
	case from == github.FromGh:
		return &widget.SetupError{Hint: problem + ". Run `gh auth login` again"}
	case from != fromKeyring:
		return &widget.SetupError{Hint: problem + ". Unset it or set a classic token with " + scopes}
	case !canStore(p):
		return &widget.SetupError{Hint: problem + ". Set GH_TOKEN to a classic token with " + scopes}
	}
	return &widget.SetupError{
		Hint:   problem + ". Run `gh auth login`, or press Enter to paste a classic token",
		Action: pasteAction(),
	}
}

func rejected(p ports.Ports, from string, err authError) *widget.SetupError {
	return unusable(p, from, from+" was rejected by GitHub: "+err.msg)
}

func lacking(p ports.Ports, from string, err scopeError) *widget.SetupError {
	return unusable(p, from, from+" needs "+scopes+". GitHub: "+strings.TrimSuffix(err.msg, "."))
}

func refused(p ports.Ports, from string) *widget.SetupError {
	return unusable(p, from, from+" is a fine-grained token, which can't search other organizations' PRs")
}

func pasteAction() *widget.Action {
	return &widget.Action{
		ID: pasteToken, Label: "paste token", Input: widget.SecretInput,
		Prompt: "Paste a classic token with " + scopes, Target: tokenPage,
	}
}

func store(p ports.Ports, text string) (widget.Result, error) {
	tok := strings.TrimSpace(text)
	switch {
	case tok == "":
		return widget.Result{}, errors.New("no token pasted")
	case fineGrained(tok):
		return widget.Result{}, errors.New("Use a classic token with " + scopes)
	case p.Keyring == nil:
		return widget.Result{}, errors.New("no keyring to keep the token in; set GH_TOKEN instead")
	}
	if err := p.Keyring.Set(service, credential, tok); err != nil {
		return widget.Result{}, fmt.Errorf("couldn't keep the token in the keyring: %w", err)
	}
	return widget.Result{Toast: "Saved the GitHub token to the keyring"}, nil
}
