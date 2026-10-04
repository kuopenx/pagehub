package server

import (
	"bytes"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"github.com/kuopenx/pagehub/internal/config"
)

const DefaultTokenName = "default"

var tokenNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`)
var ErrTokenNotFound = errors.New("named token not found")

// TokenInfo deliberately has no credential field: lists never reveal tokens.
type TokenInfo struct {
	Name      string `json:"name"`
	State     string `json:"state"`
	CreatedAt string `json:"created_at,omitempty"`
	RevokedAt string `json:"revoked_at,omitempty"`
}

type storedToken struct {
	TokenInfo
	Token string `json:"token,omitempty"`
}

type tokenRegistry struct {
	SchemaVersion int           `json:"schema_version"`
	Tokens        []storedToken `json:"tokens"`
}

func ValidateTokenName(name string) error {
	if !tokenNamePattern.MatchString(name) {
		return errors.New("token name must be 1..64 letters, digits, dots, underscores, or hyphens, starting with a letter or digit")
	}
	return nil
}

func registryPath(path string) string { return filepath.Join(filepath.Dir(path), "tokens.json") }

func readRegistry(path string) (tokenRegistry, error) {
	r := tokenRegistry{SchemaVersion: 1, Tokens: []storedToken{}}
	b, err := os.ReadFile(registryPath(path))
	if errors.Is(err, os.ErrNotExist) {
		return r, nil
	}
	if err != nil {
		return r, err
	}
	r = tokenRegistry{} // Existing files must explicitly provide both schema fields.
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&r); err != nil {
		return r, errors.New("invalid token registry")
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF || r.SchemaVersion != 1 || r.Tokens == nil {
		return r, errors.New("invalid token registry")
	}
	seen := map[string]bool{}
	for _, entry := range r.Tokens {
		if ValidateTokenName(entry.Name) != nil || entry.Name == DefaultTokenName || seen[entry.Name] {
			return r, errors.New("invalid token registry")
		}
		seen[entry.Name] = true
		if _, err := time.Parse(time.RFC3339Nano, entry.CreatedAt); err != nil {
			return r, errors.New("invalid token registry")
		}
		switch entry.State {
		case "active":
			_, err := hex.DecodeString(entry.Token)
			if len(entry.Token) != 64 || err != nil || entry.RevokedAt != "" {
				return r, errors.New("invalid token registry")
			}
		case "revoked":
			if _, err := time.Parse(time.RFC3339Nano, entry.RevokedAt); err != nil || entry.Token != "" {
				return r, errors.New("invalid token registry")
			}
		default:
			return r, errors.New("invalid token registry")
		}
	}
	return r, nil
}

// AcceptToken preserves the legacy credential independently of the registry.
// A missing/malformed/revoked default does not disable valid named tokens, and
// a malformed registry never grants access through a named token.
func AcceptToken(path, candidate string) bool {
	if candidate == "" {
		return false
	}
	if token, err := ReadToken(path); err == nil && token != "" && subtle.ConstantTimeCompare([]byte(candidate), []byte(token)) == 1 {
		return true
	}
	r, err := readRegistry(path)
	if err != nil {
		return false
	}
	match := 0
	for _, entry := range r.Tokens {
		if entry.State == "active" {
			match |= subtle.ConstantTimeCompare([]byte(candidate), []byte(entry.Token))
		}
	}
	return match == 1
}

func ReadNamedToken(path, name string) (string, TokenInfo, error) {
	info := TokenInfo{Name: name, State: "missing"}
	if err := ValidateTokenName(name); err != nil {
		return "", info, err
	}
	if name == DefaultTokenName {
		token, err := ReadToken(path)
		if errors.Is(err, os.ErrNotExist) {
			return "", info, ErrTokenNotFound
		}
		if err == nil {
			info.State = "revoked"
			if token != "" {
				info.State = "active"
			}
		}
		return token, info, err
	}
	r, err := readRegistry(path)
	if err != nil {
		return "", info, err
	}
	for _, entry := range r.Tokens {
		if entry.Name == name {
			return entry.Token, entry.TokenInfo, nil
		}
	}
	return "", info, ErrTokenNotFound
}

func ListTokens(path string) ([]TokenInfo, error) {
	_, info, err := ReadNamedToken(path, DefaultTokenName)
	if err != nil && !errors.Is(err, ErrTokenNotFound) {
		return nil, err
	}
	infos := []TokenInfo{info}
	r, err := readRegistry(path)
	if err != nil {
		return nil, err
	}
	for _, entry := range r.Tokens {
		infos = append(infos, entry.TokenInfo)
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].Name < infos[j].Name })
	return infos, nil
}

func ChangeNamedToken(path, name, action string) (string, error) {
	if err := ValidateTokenName(name); err != nil {
		return "", err
	}
	if action != "generate" && action != "rotate" && action != "revoke" {
		return "", errors.New("invalid token action")
	}
	if name == DefaultTokenName {
		return changeToken(path, action)
	}
	return withTokenLock(path, func() (string, error) {
		r, err := readRegistry(path)
		if err != nil {
			return "", err
		}
		index := -1
		for i, entry := range r.Tokens {
			if entry.Name == name {
				index = i
				break
			}
		}
		if index < 0 {
			if action != "generate" {
				return "", ErrTokenNotFound
			}
			r.Tokens = append(r.Tokens, storedToken{TokenInfo: TokenInfo{Name: name}})
			index = len(r.Tokens) - 1
		}
		entry := &r.Tokens[index]
		if action == "generate" && entry.State == "active" {
			return entry.Token, nil
		}
		if action == "revoke" {
			if entry.State == "revoked" {
				return "", nil
			}
			entry.State, entry.Token = "revoked", ""
			entry.RevokedAt = time.Now().UTC().Format(time.RFC3339Nano)
		} else {
			entry.Token, err = randomToken()
			if err != nil {
				return "", err
			}
			entry.State, entry.RevokedAt = "active", ""
			entry.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
		}
		b, err := json.MarshalIndent(r, "", "  ")
		if err != nil {
			return "", fmt.Errorf("encode token registry: %w", err)
		}
		if err := config.AtomicWrite(registryPath(path), append(b, '\n'), 0600); err != nil {
			return "", err
		}
		return entry.Token, nil
	})
}
