# `proxy`

`proxy` 是一个通用动态反向代理包。

它不关心请求里的业务语义，只要求调用方提供一个 `proxyplan.Resolver`，把传入请求解析成 `proxyplan.Plan`。

## Core Types

- `proxyplan.Resolver`
  从传入请求生成 `proxyplan.Plan`
- `proxyplan.Plan`
  描述本次请求该发往哪里，以及如何改写 header
- `proxyplan.HeaderOp`
  表示单个 header 操作，支持 `=`, `+`, `-`
- `proxyplan.HeaderMode`
  控制 header 基底，`patch` 保留入站 headers，`replace` 清空后重建

## Minimal Usage

```go
type staticResolver struct{}

func (staticResolver) Resolve(r *http.Request) (*proxyplan.Plan, error) {
	target, _ := url.Parse("https://api.example.com/v1")
	return &proxyplan.Plan{
		Target:   target,
		JoinPath: true,
		HeaderOps: []proxyplan.HeaderOp{
			{
				Action: proxyplan.HeaderSet,
				Name:   "Authorization",
				Values: []string{"Bearer upstream-token"},
			},
		},
	}, nil
}

handler := proxy.NewHandler(staticResolver{}, http.DefaultTransport, proxy.HandlerOptions{})
```

## Header Operations

- `HeaderModePatch`
  基于入站 request headers 按顺序修改
- `HeaderModeReplace`
  先清空 outbound headers，再按顺序应用操作
- `HeaderSet`
  覆盖 header 值
- `HeaderAdd`
  追加 header 值
- `HeaderRemove`
  删除匹配值；若没有剩余值则删除整个 header
- `Host`
  作为特殊 header 写入 `Request.Host`

## Design Notes

- `proxy` 只做代理执行，不做协议解释。
- 上游 transport 关闭隐式 gzip，避免自动注入 `Accept-Encoding`。
- LLM Bearer 解析、JWT 解析、租户路由等都应该放在各自的 resolver 里，并输出 `internal/proxyplan` 的通用执行计划。
- 它可以和 `internal/plugins/capture` 组合使用，把动态代理和 capture 传输层拼起来。
