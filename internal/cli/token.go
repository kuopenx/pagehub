package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/kuopenx/pagehub/internal/server"
)

func runToken(action string, o options, out, errOut io.Writer) int {
	if o.port != 0 || o.label != "" {
		fmt.Fprintln(errOut, "token commands accept --data-dir and --json, not --port or --service-name")
		return 2
	}
	name := o.tokenName
	if name == "" {
		name = server.DefaultTokenName
	}
	if err := server.ValidateTokenName(name); err != nil {
		fmt.Fprintln(errOut, err)
		return 2
	}
	if action == "list" && o.tokenName != "" {
		fmt.Fprintln(errOut, "token list does not accept --name")
		return 2
	}
	path := filepath.Join(o.dir, "token")
	r := result{Command: "token " + action, TokenName: name}
	var err error
	switch action {
	case "generate", "rotate", "revoke":
		r.Token, err = server.ChangeNamedToken(path, name, action)
		if action == "revoke" {
			r.TokenState = "revoked"
		}
	case "show", "status":
		var token string
		var info server.TokenInfo
		token, info, err = server.ReadNamedToken(path, name)
		r.TokenState = info.State
		if errors.Is(err, server.ErrTokenNotFound) && action == "status" {
			err = nil
		}
		if err == nil && action == "show" {
			if token == "" {
				err = errors.New("token revoked; run pagehub token generate with the same --name")
			} else {
				r.Token = token
			}
		}
	case "list":
		r.TokenName = ""
		r.Tokens, err = server.ListTokens(path)
	default:
		fmt.Fprintln(errOut, "token requires generate, show, status, rotate, revoke, or list")
		return 2
	}
	if err != nil {
		if o.json {
			_ = json.NewEncoder(errOut).Encode(map[string]string{"error": err.Error()})
		} else {
			fmt.Fprintln(errOut, "pagehub:", err)
		}
		return 1
	}
	if r.Token != "" {
		r.TokenState = "active"
	}
	if o.json {
		err = json.NewEncoder(out).Encode(r)
	} else if action == "list" {
		_, err = fmt.Fprintln(out, "NAME\tSTATE")
		for _, info := range r.Tokens {
			if err != nil {
				break
			}
			_, err = fmt.Fprintf(out, "%s\t%s\n", info.Name, info.State)
		}
	} else if r.Token != "" {
		// Only explicitly requested credential commands reveal the token, as one
		// line that can be copied or piped into a clipboard utility.
		_, err = fmt.Fprintln(out, r.Token)
	} else {
		_, err = fmt.Fprintln(out, r.TokenState)
	}
	if err != nil {
		// A writer's error may contain the output, so do not include it in the
		// diagnostic: generate/show/rotate output contains credentials.
		const message = "could not write token command output"
		if o.json {
			_ = json.NewEncoder(errOut).Encode(map[string]string{"error": message})
		} else {
			fmt.Fprintln(errOut, "pagehub:", message)
		}
		return 1
	}
	return 0
}
