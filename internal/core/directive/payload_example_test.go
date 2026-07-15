package directive_test

import (
	"fmt"

	"github.com/lwmacct/260628-directive-proxy/internal/core/directive"
)

func ExampleEncode() {
	encoded, _ := directive.Encode(directive.Payload{
		Target: directive.TargetSection{URL: "https://api.example.com/v1"},
		Headers: &directive.HeaderSection{Request: &directive.RequestHeaderSection{Ops: []directive.HeaderOp{
			{Op: "=", Name: "Authorization", Values: []string{"Bearer upstream-token"}},
			{Op: "=", Name: "X-Tenant", Values: []string{"tenant-a"}},
		}}},
	})
	fmt.Println(len(encoded) > 0)
	// Output:
	// true
}
