package handler

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAsyncUsageEntryPointsCarryFrozenRequestPricingAt(t *testing.T) {
	tests := []struct {
		file              string
		inputType         string
		wantPricingHelper string
		wantHelperCalls   int
	}{
		{file: "openai_images.go", inputType: "OpenAIRecordUsageInput", wantPricingHelper: "WithOpenAIRequestPricingContext", wantHelperCalls: 1},
		{file: "grok_media.go", inputType: "OpenAIRecordUsageInput", wantPricingHelper: "WithOpenAIRequestPricingContext", wantHelperCalls: 1},
		{file: "grok_audio.go", inputType: "OpenAIRecordUsageInput", wantPricingHelper: "WithOpenAIRequestPricingContext", wantHelperCalls: 2},
		{file: "gateway_web_search.go", inputType: "RecordUsageInput", wantPricingHelper: "WithGatewayTokenRequestPricing", wantHelperCalls: 1},
	}

	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, filepath.Join(".", tt.file), nil, 0)
			require.NoError(t, err)

			helperCalls := 0
			usageInputs := 0
			ast.Inspect(file, func(node ast.Node) bool {
				switch current := node.(type) {
				case *ast.CallExpr:
					if selectorName(current.Fun) == tt.wantPricingHelper {
						helperCalls++
					}
				case *ast.CompositeLit:
					if selectorName(current.Type) != tt.inputType {
						return true
					}
					usageInputs++
					pricingAt, ok := compositeLiteralValue(current, "PricingAt")
					require.True(t, ok, "%s usage input must carry request-level PricingAt", fset.Position(current.Lbrace))
					ident, ok := pricingAt.(*ast.Ident)
					require.True(t, ok, "%s PricingAt must use the frozen request scalar", fset.Position(pricingAt.Pos()))
					require.Equal(t, "pricingAt", ident.Name)
				}
				return true
			})

			require.Equal(t, tt.wantHelperCalls, helperCalls, "each request entry must freeze PricingAt once")
			require.Positive(t, usageInputs, "expected at least one production usage input")
		})
	}
}

func TestAsyncUsagePricingAtRemainsRequestScopedAfterDetach(t *testing.T) {
	requestCtx, cancelRequest := context.WithCancel(context.Background())
	openAICtx, openAIAt := (&service.OpenAIGatewayService{}).WithOpenAIRequestPricingContext(
		service.WithOpenAIProfitControlSuppressed(requestCtx), nil,
	)
	gatewayCtx, gatewayAt := service.WithGatewayTokenRequestPricing(requestCtx)

	require.False(t, openAIAt.IsZero())
	require.Equal(t, openAIAt, service.OpenAIPricingAtFromContext(openAICtx))
	require.False(t, gatewayAt.IsZero())
	require.Equal(t, gatewayAt, service.GatewayTokenRequestPricingAtFromContext(gatewayCtx))

	cancelRequest()
	time.Sleep(time.Millisecond)

	var gotOpenAIAt time.Time
	var gotGatewayAt time.Time
	wrapUsageRecordTaskContext(openAICtx, func(context.Context) {
		gotOpenAIAt = openAIAt
	})(context.Background())
	wrapUsageRecordTaskContext(gatewayCtx, func(context.Context) {
		gotGatewayAt = gatewayAt
	})(context.Background())

	require.Equal(t, openAIAt, gotOpenAIAt)
	require.Equal(t, gatewayAt, gotGatewayAt)
}

func selectorName(expr ast.Expr) string {
	selector, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	return selector.Sel.Name
}

func compositeLiteralValue(literal *ast.CompositeLit, key string) (ast.Expr, bool) {
	for _, elt := range literal.Elts {
		pair, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		ident, ok := pair.Key.(*ast.Ident)
		if ok && ident.Name == key {
			return pair.Value, true
		}
	}
	return nil, false
}
