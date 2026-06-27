package proxydirective_test

import (
	"fmt"

	"github.com/lwmacct/260628-llm-relay-dproxy/internal/proxydirective"
)

func ExampleEncode() {
	encoded, _ := proxydirective.Encode(proxydirective.Payload{
		Target: proxydirective.TargetSection{URL: "https://api.example.com/v1"},
		Headers: &proxydirective.HeaderSection{Ops: []proxydirective.HeaderOp{
			{Op: "=", Name: "Authorization", Values: []string{"Bearer upstream-token"}},
			{Op: "=", Name: "X-Tenant", Values: []string{"tenant-a"}},
		}},
	})
	fmt.Println(len(encoded) > 0)
	// Output:
	// true
}
