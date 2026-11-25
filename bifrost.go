package datacircle

import (
	"context"
	"encoding/json"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	bifrost "github.com/maximhq/bifrost/core"
	"github.com/maximhq/bifrost/core/schemas"
)

// ClientName is the in-process MCP client's name that MCPClientConfig gives: Bifrost shows its tools to the model as
// datacircle-datacircle_list_apis and so on.
const ClientName = "datacircle"

// Tools gives the four tools for a request's Params.Tools, beside an app's own: each a schemas.ChatTool of type
// function with its name, description and parameters. Each call gives new values, which the app may change. Execute
// runs the calls of them in the response.
func (t *Toolset) Tools() []schemas.ChatTool {
	out := make([]schemas.ChatTool, 0, len(Names))
	for _, name := range Names {
		about := abouts[name]
		out = append(out, schemas.ChatTool{Type: schemas.ChatToolTypeFunction,
			Function: &schemas.ChatToolFunction{Name: name, Description: &about, Parameters: parameters(name)}})
	}
	return out
}

// Execute runs one tool call of a chat response, from the tools Tools gives, into the message that answers it, for the
// next request's Input: role tool, its ToolCallID, and as its content the tool's JSON, or {"error": ...} with IsError
// set, the API's words or the failure's, which the model reads and can call again on. Its false is a call of none of the
// four tools, for the app to run.
func (t *Toolset) Execute(ctx context.Context, call schemas.ChatAssistantMessageToolCall) (schemas.ChatMessage, bool) {
	if call.Function.Name == nil || !t.Handles(*call.Function.Name) {
		return schemas.ChatMessage{}, false
	}
	out, err := t.Run(ctx, *call.Function.Name, call.Function.Arguments)
	failed := err != nil
	if failed {
		b, _ := json.Marshal(map[string]string{"error": err.Error()})
		out = string(b)
	}
	return schemas.ChatMessage{Role: schemas.ChatMessageRoleTool, Content: &schemas.ChatMessageContent{ContentStr: &out},
		ChatToolMessage: &schemas.ChatToolMessage{ToolCallID: call.ID, IsError: &failed}}, true
}

// Register adds the four tools to Bifrost's own in-process MCP server (its RegisterMCPTool): Bifrost then gives them to
// the model in every chat and responses request, as bifrostInternal-datacircle_list_apis and so on, and the app runs a
// call of them with client.ExecuteChatMCPTool or client.ExecuteResponsesMCPTool. The client needs an MCPConfig, even an
// empty one. A failed call gives the model Error: and its words.
func (t *Toolset) Register(client *bifrost.Bifrost) error {
	for _, tool := range t.Tools() {
		name := tool.Function.Name
		handler := func(args any) (string, error) {
			arguments, err := json.Marshal(args)
			if err != nil {
				return "", err
			}
			ctx, cancel := context.WithTimeout(context.Background(), t.c.timeout)
			defer cancel()
			return t.Run(ctx, name, string(arguments))
		}
		if err := client.RegisterMCPTool(name, abouts[name], handler, tool); err != nil {
			return err
		}
	}
	return nil
}

// MCPServer gives an mcp-go server that holds the four tools, each read-only, with its title, for an in-process MCP
// client of Bifrost's. MCPClientConfig gives that client.
func (t *Toolset) MCPServer() *server.MCPServer {
	s := server.NewMCPServer("datacircle", Version, server.WithToolCapabilities(false))
	for _, name := range Names {
		p := parameters(name)
		props := map[string]any{}
		for _, key := range p.Properties.Keys() {
			props[key], _ = p.Properties.Get(key)
		}
		tool := mcp.Tool{Name: name, Description: abouts[name],
			InputSchema: mcp.ToolInputSchema{Type: "object", Properties: props, Required: p.Required},
			Annotations: mcp.ToolAnnotation{Title: titles[name], ReadOnlyHint: hint(true), DestructiveHint: hint(false),
				IdempotentHint: hint(true), OpenWorldHint: hint(true)}}
		s.AddTool(tool, t.handler(name))
	}
	return s
}

// MCPClientConfig gives an in-process MCP client named datacircle on MCPServer, for an Account's MCP clients or
// client.AddMCPClient. Its four tools are all allowed and run by themselves in Bifrost's agent mode (ToolsToExecute and
// ToolsToAutoExecute ["*"]): change either list on the value it gives.
func (t *Toolset) MCPClientConfig() *schemas.MCPClientConfig {
	return &schemas.MCPClientConfig{ID: ClientName, Name: ClientName, ConnectionType: schemas.MCPConnectionTypeInProcess,
		InProcessServer: t.MCPServer(), ToolsToExecute: []string{"*"}, ToolsToAutoExecute: []string{"*"}}
}

func hint(v bool) *bool { return &v }

// handler answers an MCP call of one tool: its result as JSON text, or an error result (isError) with the API's words or
// the failure's. A call its client canceled gives the context's error.
func (t *Toolset) handler(name string) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		arguments, err := json.Marshal(req.GetArguments())
		if err != nil {
			return mcp.NewToolResultError("The arguments are no JSON object of this tool's parameters: " + err.Error()), nil
		}
		out, err := t.Run(ctx, name, string(arguments))
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(out), nil
	}
}
