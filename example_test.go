package datacircle_test

import (
	"context"
	"fmt"

	bifrost "github.com/maximhq/bifrost/core"
	"github.com/maximhq/bifrost/core/schemas"
	datacircle "github.com/waynehamadi/bifrost-datacircle"
)

var (
	account schemas.Account
	input   []schemas.ChatMessage
)

// The four tools in a request's Params.Tools, beside an app's own, each call of them run by Execute.
func ExampleToolset_Tools() {
	ctx := context.Background()
	bctx := schemas.NewBifrostContext(ctx, schemas.NoDeadline)
	client, _ := bifrost.Init(ctx, schemas.BifrostConfig{Account: account})
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
}

// The four tools in Bifrost's own MCP server, each call of them run by ExecuteChatMCPTool.
func ExampleToolset_Register() {
	ctx := context.Background()
	bctx := schemas.NewBifrostContext(ctx, schemas.NoDeadline)
	client, _ := bifrost.Init(ctx, schemas.BifrostConfig{Account: account, MCPConfig: &schemas.MCPConfig{}})
	datacircle.New(nil).Register(client)
	req := &schemas.BifrostChatRequest{Provider: schemas.OpenAI, Model: "gpt-4o-mini", Input: input}
	res, _ := client.ChatCompletionRequest(bctx, req)
	for _, call := range res.Choices[0].Message.ToolCalls {
		msg, _ := client.ExecuteChatMCPTool(bctx, &call)
		input = append(input, *msg)
	}
}

// The four tools as an in-process MCP client, run by Bifrost itself in agent mode.
func ExampleToolset_MCPClientConfig() {
	ctx := context.Background()
	bctx := schemas.NewBifrostContext(ctx, schemas.NoDeadline)
	client, _ := bifrost.Init(ctx, schemas.BifrostConfig{Account: account, MCPConfig: &schemas.MCPConfig{}})
	client.AddMCPClient(ctx, datacircle.New(nil).MCPClientConfig())
	req := &schemas.BifrostChatRequest{Provider: schemas.OpenAI, Model: "gpt-4o-mini", Input: input}
	res, _ := client.ChatCompletionRequest(bctx, req) // the tools run by themselves
	fmt.Println(*res.Choices[0].Message.Content.ContentStr)
}
