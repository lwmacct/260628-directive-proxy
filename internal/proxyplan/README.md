# `proxyplan`

`proxyplan` defines the canonical per-request proxy execution plan.

It owns the common types shared by directive resolvers, reverse proxy executor,
and capture transport:

- `Plan`: target, outbound proxy, header mode, header operations, labels, capture policy, and path behavior
- `Resolver`: incoming request to plan contract
- `CapturePolicy`: per-request opt-in capture settings
- request context helpers for passing the resolved plan through the proxy stack

Protocol-specific parsing belongs outside this package. `internal/proxydirective`
parses `X-Proxy-Directive` and `Authorization: Bearer`, then converts the
payload into these types.

Header mode controls the base header set:

- `patch`: keep inbound headers and apply operations in order
- `replace`: clear outbound headers first, then apply operations in order
