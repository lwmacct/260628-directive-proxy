package directive_test

import (
	"fmt"

	"github.com/lwmacct/260628-directive-proxy/internal/core/directive"
)

func ExampleEncode() {
	encoded, _ := directive.Encode("example-directive-secret", directive.Payload{
		Metadata: map[string]string{"user_key": "uk_example"},
		Target:   directive.TargetSection{BaseURL: "https://api.example.com/v1"},
		Headers: &directive.HeaderPolicy{Mutations: []directive.HeaderMutation{
			{Side: directive.HeaderSideRequest, Action: directive.HeaderActionSet, Name: "Authorization", Values: []string{"Bearer upstream-token"}},
			{Side: directive.HeaderSideRequest, Action: directive.HeaderActionSet, Name: "X-Tenant", Values: []string{"tenant-a"}},
		}},
	})
	fmt.Println(len(encoded) > 0)
	// Output:
	// true
}
