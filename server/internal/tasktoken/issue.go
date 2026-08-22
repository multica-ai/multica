package tasktoken

import (
	"crypto"
	"crypto/rand"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Identity is the resolved accountable human a token speaks for. It is
// produced by the server from the task row — never from anything the agent
// or the daemon can influence.
type Identity struct {
	Email  string
	Name   string
	UserID string
	// Source is the attribution waterfall level that resolved this human
	// (direct_human, rule_owner, ...). Carried so a deployment can mark
	// automation-originated requests in its own claims.
	Source string
}

// Context carries every value a claims template may interpolate.
type Context struct {
	Identity      Identity
	WorkspaceID   string
	WorkspaceSlug string
}

// EmailLocal returns the local part of an address, lowercased, with any "+tag"
// suffix removed: "A+bug@Example.com" -> "a". An address with no "@" is
// treated as already being a local part.
func EmailLocal(email string) string {
	local := email
	if at := strings.IndexByte(local, '@'); at >= 0 {
		local = local[:at]
	}
	local = strings.ToLower(local)
	if plus := strings.IndexByte(local, '+'); plus >= 0 {
		local = local[:plus]
	}
	return local
}

// Issuer signs catalog templates for a resolved identity.
type Issuer struct {
	catalog     *Catalog
	key         crypto.PrivateKey
	manifestEnv string
}

// NewIssuer builds an Issuer from the raw catalog JSON and PEM private key.
// Both blank means the feature is off and it returns (nil, nil). Exactly one
// blank is a misconfiguration and returns an error — a deployment that set a
// key but no catalog (or vice versa) meant to enable this and should be told
// it is not enabled, not left silently disabled.
//
// manifestEnv, when non-empty, names an extra environment variable carrying a
// JSON array of the manifests of the templates actually issued for a run. It
// is optional; leaving it blank injects nothing beyond the tokens themselves.
func NewIssuer(rawCatalog, rawPrivateKey, manifestEnv string) (*Issuer, error) {
	rawCatalog = strings.TrimSpace(rawCatalog)
	rawPrivateKey = strings.TrimSpace(rawPrivateKey)
	manifestEnv = strings.TrimSpace(manifestEnv)

	switch {
	case rawCatalog == "" && rawPrivateKey == "":
		return nil, nil
	case rawPrivateKey == "":
		return nil, fmt.Errorf("task token catalog is configured but the private key is empty")
	case rawCatalog == "":
		return nil, fmt.Errorf("task token private key is configured but the catalog is empty")
	}

	catalog, err := ParseCatalog(rawCatalog)
	if err != nil {
		return nil, err
	}

	key, err := parsePrivateKey([]byte(rawPrivateKey))
	if err != nil {
		return nil, err
	}

	if manifestEnv != "" {
		if err := ValidateEnvName(manifestEnv); err != nil {
			return nil, fmt.Errorf("task token manifest env: %w", err)
		}
	}
	return &Issuer{catalog: catalog, key: key, manifestEnv: manifestEnv}, nil
}

// parsePrivateKey accepts PKCS#8 ("PRIVATE KEY"), SEC 1 EC ("EC PRIVATE KEY")
// and PKCS#1 RSA ("RSA PRIVATE KEY") PEM blocks.
func parsePrivateKey(pemBytes []byte) (crypto.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("task token private key: no PEM block found")
	}
	switch block.Type {
	case "PRIVATE KEY":
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("task token private key: %w", err)
		}
		return key, nil
	case "EC PRIVATE KEY":
		key, err := x509.ParseECPrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("task token private key: %w", err)
		}
		return key, nil
	case "RSA PRIVATE KEY":
		key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("task token private key: %w", err)
		}
		return key, nil
	default:
		return nil, fmt.Errorf("task token private key: unsupported PEM block %q", block.Type)
	}
}

// Catalog exposes the parsed catalog so the API can list what may be enabled.
func (i *Issuer) Catalog() *Catalog {
	if i == nil {
		return nil
	}
	return i.catalog
}

// Issue signs every enabled template it can and returns env name -> token.
//
// Issuing is best-effort by design: a template that has been removed from the
// catalog, or one that fails to sign, is skipped with a log line while the
// rest still reach the agent. Failing to obtain a token is an "unauthorized"
// condition for the skill that wanted it, never a task failure.
func (i *Issuer) Issue(enabledIDs []string, tctx Context, now time.Time) map[string]string {
	if i == nil || i.catalog == nil || len(enabledIDs) == 0 {
		return nil
	}

	out := make(map[string]string, len(enabledIDs)+1)
	manifests := make([]json.RawMessage, 0, len(enabledIDs))
	for _, id := range enabledIDs {
		tpl, ok := i.catalog.Get(id)
		if !ok {
			slog.Warn("task token: enabled template is not in the catalog", "template_id", id)
			continue
		}
		token, jti, err := i.sign(tpl, tctx, now)
		if err != nil {
			slog.Error("task token: signing failed", "template_id", id, "error", err)
			continue
		}
		slog.Info("task token issued",
			"template_id", tpl.ID, "env", tpl.Env, "jti", jti,
			"identity_source", tctx.Identity.Source, "user_id", tctx.Identity.UserID)
		out[tpl.Env] = token
		// Collected only for templates that actually produced a token, so the
		// manifest can never advertise a system whose token is missing.
		if len(tpl.Manifest) > 0 {
			manifests = append(manifests, tpl.Manifest)
		}
	}
	if len(out) == 0 {
		return nil
	}

	if i.manifestEnv != "" && len(manifests) > 0 {
		encoded, err := json.Marshal(manifests)
		if err != nil {
			// The tokens themselves are still good; ship them without the
			// manifest rather than failing the whole set.
			slog.Error("task token: encoding manifest failed", "error", err)
			return out
		}
		out[i.manifestEnv] = string(encoded)
	}
	return out
}

func (i *Issuer) sign(tpl Template, tctx Context, now time.Time) (token string, jti string, err error) {
	method := jwt.GetSigningMethod(tpl.Algorithm)
	if method == nil {
		return "", "", fmt.Errorf("unknown signing method %q", tpl.Algorithm)
	}

	jti, err = newJTI()
	if err != nil {
		return "", "", err
	}

	claims := jwt.MapClaims{}
	for name, value := range tpl.Claims {
		claims[name] = interpolate(value, tctx)
	}
	claims["iat"] = now.Unix()
	claims["exp"] = now.Add(tpl.TTL).Unix()
	claims["jti"] = jti

	t := jwt.NewWithClaims(method, claims)
	if tpl.KeyID != "" {
		t.Header["kid"] = tpl.KeyID
	}
	signed, err := t.SignedString(i.key)
	if err != nil {
		return "", "", err
	}
	return signed, jti, nil
}

// interpolate replaces every known {{variable}} in value. Unknown variables
// cannot reach here: ParseCatalog rejects them at startup.
func interpolate(value string, tctx Context) string {
	return variablePattern.ReplaceAllStringFunc(value, func(match string) string {
		name := variablePattern.FindStringSubmatch(match)[1]
		switch name {
		case "identity.email":
			return tctx.Identity.Email
		case "identity.email_local":
			return EmailLocal(tctx.Identity.Email)
		case "identity.name":
			return tctx.Identity.Name
		case "identity.id":
			return tctx.Identity.UserID
		case "identity.source":
			return tctx.Identity.Source
		case "workspace.id":
			return tctx.WorkspaceID
		case "workspace.slug":
			return tctx.WorkspaceSlug
		default:
			return ""
		}
	})
}

func newJTI() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate jti: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
