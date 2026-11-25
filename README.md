# Datacircle tools for Bifrost

Find the best GTM APIs. We benchmark all the GTM APIs and we put the results on our website: https://datacircle.dev

This Go module gives [Bifrost](https://github.com/maximhq/bifrost)'s Go SDK four tools on Datacircle's open API, https://api.datacircle.dev/v1. You need no key and no account.

| Tool | What the tool gives | Call |
| --- | --- | --- |
| `datacircle_list_apis` | Every GTM API on Datacircle, with the API's slug, number of plans, number of providers and best plan | `GET /apis` |
| `datacircle_best_plans` | One API's plans, sorted by score out of 100, best first. Each plan comes with a rank and a price per 1,000 calls | `GET /apis/{slug}/best` |
| `datacircle_search_plans` | Plans that match a provider or plan name, on all APIs or on one API. Sort by score, provider or price | `GET /plans` |
| `datacircle_provider` | The APIs a provider sells, and the provider's rank on each API | `GET /providers/{slug}` |

## Install

```bash
go get github.com/waynehamadi/bifrost-datacircle
```

It needs Bifrost core v1.14.0 or later. Core v1.15.0 and later build only with Bifrost's own fork of mcp-go, so add the line Bifrost's `go.mod` has to yours:

```
replace github.com/mark3labs/mcp-go => github.com/maximhq/mcp-go v0.0.0-20261102181017-b7da3bc145a1
```

## Use the tools

There are three ways. Pick one per client. `bctx` is a `*schemas.BifrostContext`, as from `schemas.NewBifrostContext(ctx, schemas.NoDeadline)`.

**In a request's tools.** `Tools()` gives the four tools as `schemas.ChatTool`, beside your own. `Execute` runs the model's calls of them and gives the tool message for the next request:

```go
tools := datacircle.New(nil)
req := &schemas.BifrostChatRequest{Provider: schemas.OpenAI, Model: "gpt-4o-mini", Input: input,
	Params: &schemas.ChatParameters{Tools: tools.Tools()}}
res, _ := client.ChatCompletionRequest(bctx, req)
msg := res.Choices[0].Message
input = append(input, *msg)
for _, call := range msg.ToolCalls {
	if answer, ok := tools.Execute(ctx, call); ok {
		input = append(input, answer)
	}
}
```

`Execute` gives `false` for a call of a tool that isn't one of the four, so your app runs it.

**In Bifrost's own MCP server.** `Register` adds the four tools to the client's in-process MCP server (`RegisterMCPTool`). Bifrost then gives them to the model in every chat and responses request, as `bifrostInternal-datacircle_list_apis` and so on. Your app runs the calls with `ExecuteChatMCPTool` or `ExecuteResponsesMCPTool`:

```go
client, _ := bifrost.Init(ctx, schemas.BifrostConfig{Account: account, MCPConfig: &schemas.MCPConfig{}})
datacircle.New(nil).Register(client)
res, _ := client.ChatCompletionRequest(bctx, req)
for _, call := range res.Choices[0].Message.ToolCalls {
	msg, _ := client.ExecuteChatMCPTool(bctx, &call)
	input = append(input, *msg)
}
```

**As an MCP client, run by Bifrost itself.** `MCPClientConfig()` gives an in-process MCP client named `datacircle`. Bifrost shows its tools to the model as `datacircle-datacircle_list_apis` and so on, and runs them by itself in agent mode: `ChatCompletionRequest` and `ResponsesRequest` give back the model's answer once it has the tools' results.

```go
client, _ := bifrost.Init(ctx, schemas.BifrostConfig{Account: account, MCPConfig: &schemas.MCPConfig{}})
client.AddMCPClient(ctx, datacircle.New(nil).MCPClientConfig())
res, _ := client.ChatCompletionRequest(bctx, req) // the tools run by themselves
```

The config allows the four tools and runs them by itself (`ToolsToExecute` and `ToolsToAutoExecute` are `["*"]`). Change either list on the value it gives. It can also go in `MCPConfig.ClientConfigs`, connected by `ConnectConfiguredMCPClients`.

Each tool gives back a JSON object. Each plan comes with its rank, provider, plan, score, price per 1,000 (`perThousand`, in dollars) and its page on datacircle.dev. That page lists every field. `datacircle_provider` starts with a sentence of its counts: the APIs where the provider has plans, those where it has a rank, and its best rank.

When the API gives no data, like for a slug it doesn't know, the tool's result is an error with the API's own words and what to call instead. The model reads it and can call again. `Toolset.Run(ctx, name, arguments)` calls one tool yourself.

`datacircle.Config` sets the `HTTPClient` the tools use (a timeout, a proxy) and `Timeout`, the most one call may take when Bifrost runs it (2 minutes when 0). `nil` uses `http.DefaultClient`.

## Network

Each tool call makes one HTTPS GET request to https://api.datacircle.dev/v1 and nothing else. The request holds only the tool's own input: no key, no config, nothing from your chats. The API takes up to 60 calls a minute from one IP address. The tool waits once, for a minute at most, when the API asks the tool to wait. The module depends only on Bifrost core, mcp-go and `golang.org/x/text`, and stores nothing.

## Links

- Datacircle: https://datacircle.dev
- The API: https://datacircle.dev/api
- The API's spec: https://api.datacircle.dev/v1/openapi.json
- What each key means: https://datacircle.dev/fields

MIT license.
