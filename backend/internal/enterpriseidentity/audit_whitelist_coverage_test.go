package enterpriseidentity

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestAuditPayloadFieldWhitelistCoversAllWriteSites is a mechanical guard
// against B2-class regressions: every enterprise_audit_events writer builds
// its payload as a `map[string]any{...}` literal, so this test parses every
// non-test .go file in this package and in ../enterprise, collects every
// string key used in such a literal, and asserts auditPayloadFieldWhitelist
// (workbench.go) covers all of them. A new audit writer that introduces a
// field without updating the whitelist now fails the build instead of
// silently disappearing from the detail drawer.
func TestAuditPayloadFieldWhitelistCoversAllWriteSites(t *testing.T) {
	dirs := []string{".", "../enterprise"}
	fset := token.NewFileSet()
	found := map[string][]string{} // field -> files it was seen in, for a readable failure message

	for _, dir := range dirs {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		require.NoError(t, err)
		for _, file := range files {
			if strings.HasSuffix(file, "_test.go") {
				continue
			}
			astFile, err := parser.ParseFile(fset, file, nil, 0)
			require.NoError(t, err, "parse %s", file)

			ast.Inspect(astFile, func(n ast.Node) bool {
				lit, ok := n.(*ast.CompositeLit)
				if !ok {
					return true
				}
				mapType, ok := lit.Type.(*ast.MapType)
				if !ok {
					return true
				}
				keyIdent, ok := mapType.Key.(*ast.Ident)
				if !ok || keyIdent.Name != "string" {
					return true
				}
				switch v := mapType.Value.(type) {
				case *ast.Ident:
					if v.Name != "any" {
						return true
					}
				case *ast.InterfaceType:
					if v.Methods != nil && len(v.Methods.List) > 0 {
						return true
					}
				default:
					return true
				}
				for _, elt := range lit.Elts {
					kv, ok := elt.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					keyLit, ok := kv.Key.(*ast.BasicLit)
					if !ok || keyLit.Kind != token.STRING {
						continue
					}
					field, err := strconv.Unquote(keyLit.Value)
					if err != nil {
						continue
					}
					found[field] = append(found[field], file)
				}
				return true
			})
		}
	}

	require.NotEmpty(t, found, "expected to discover at least one enterprise_audit_events payload literal across %v", dirs)

	var missing []string
	for field, files := range found {
		if _, ok := auditPayloadFieldWhitelist[field]; !ok {
			missing = append(missing, field+" (seen in "+strings.Join(dedupe(files), ", ")+")")
		}
	}
	require.Empty(t, missing, "audit payload field(s) missing from auditPayloadFieldWhitelist in workbench.go — the admin audit detail drawer silently drops them:\n%s", strings.Join(missing, "\n"))
}

func dedupe(items []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(items))
	for _, item := range items {
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	return result
}
