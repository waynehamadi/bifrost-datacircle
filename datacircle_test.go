package datacircle

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	bifrost "github.com/maximhq/bifrost/core"
	"github.com/maximhq/bifrost/core/schemas"
)

func TestDefinitions(t *testing.T) {
	tools := New(nil)
	defs := tools.Tools()
	if len(defs) != 4 {
		t.Fatalf("%d tools, want 4", len(defs))
	}
	for i, name := range Names {
		f := defs[i].Function
		if defs[i].Type != schemas.ChatToolTypeFunction || f == nil || f.Name != name || *f.Description != abouts[name] {
			t.Fatalf("tool %d: %#v", i, defs[i])
		}
		// As Bifrost sends it to an OpenAI-style provider: the tool's JSON.
		sent, err := json.Marshal(defs[i])
		if err != nil {
			t.Fatal(err)
		}
		var got struct {
			Type     string `json:"type"`
			Function struct {
				Name       string `json:"name"`
				Parameters struct {
					Type       string                    `json:"type"`
					Properties map[string]map[string]any `json:"properties"`
					Required   []string                  `json:"required"`
				} `json:"parameters"`
			} `json:"function"`
		}
		if err := json.Unmarshal(sent, &got); err != nil {
			t.Fatal(err)
		}
		p := got.Function.Parameters
		if got.Type != "function" || got.Function.Name != name || p.Type != "object" || p.Properties["limit"]["type"] != "integer" {
			t.Errorf("%s: %s", name, sent)
		}
		for key, prop := range p.Properties {
			if prop["description"] == "" || prop["type"] == nil {
				t.Errorf("%s's %s: %s", name, key, sent)
			}
		}
		slug := name == BestPlansName || name == ProviderName
		if slug != (len(p.Required) == 1 && p.Required[0] == "slug") || !slug && len(p.Required) > 0 {
			t.Errorf("%s requires %v", name, p.Required)
		}
		if name == SearchPlansName && !strings.Contains(string(sent), `"properties":{"api":`) ||
			name == SearchPlansName && !strings.Contains(string(sent), `"enum":["score","provider","price"]`) {
			t.Errorf("%s's properties are out of order or sort has no enum: %s", name, sent)
		}
		t.Logf("%s: %s", name, sent)
	}
	if defs[0].Function == tools.Tools()[0].Function {
		t.Errorf("Tools gave the same values twice")
	}
}

func TestCalls(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.RequestURI())
		switch {
		case r.URL.Path == "/apis":
			w.Write([]byte(`[{"api":"Employee profile","slug":"linkedin-profile","sheetPage":"s"},{"api":"Employee email","slug":"email-finder","plans":3}]`))
		case r.URL.Path == "/apis/email-finder/best":
			w.Write([]byte(`{"api":"Employee email","plans":[{"provider":"A","rank":1,"terms":"long"}]}`))
		case r.URL.Path == "/plans" && (r.URL.Query().Get("api") == "Bright Data" || r.URL.Query().Get("api") == "bright-data"):
			w.WriteHeader(400)
			w.Write([]byte(`{"error":"api must be one of: Employee email"}`))
		case r.URL.Path == "/plans" && strings.Contains(r.URL.Query().Get("query"), "-"):
			w.Write([]byte(`{"total":0,"plans":[]}`))
		case r.URL.Path == "/plans":
			w.Write([]byte(`{"total":1,"plans":[{"provider":"Bright Data","rank":2}]}`))
		case r.URL.Path == "/providers/bright-data":
			w.Write([]byte(`{"provider":"Bright Data","apis":[{"api":"Employee profile","plans":[{"plan":"C"}]},{"api":"Employee email","rank":4,"plans":[{"plan":"B","rank":9},{"plan":"A","rank":4}]}]}`))
		case r.URL.Path == "/providers/down":
			w.WriteHeader(502)
		default:
			w.WriteHeader(404)
			w.Write([]byte(`{"error":"no API has the slug nope"}`))
		}
	}))
	defer server.Close()
	tools := New(&Config{BaseURL: server.URL})
	ctx := context.Background()
	for _, call := range []struct {
		name, arguments string
		fails           bool
		has             string
	}{
		{ListAPIsName, `{"query":"email"}`, false, `"total":1`},
		{ListAPIsName, `{}`, false, `"total":2`},
		{BestPlansName, `{"slug":"Email Finder","limit":"2"}`, false, `"provider":"A"`},
		{SearchPlansName, `{"api":"Bright Data"}`, false, `No API is named`},
		{SearchPlansName, `{"api":null,"query":null,"sort":null,"limit":null,"offset":null}`, false, `"total":1`},
		{SearchPlansName, `{"api":"Bright Data","query":"growth"}`, true, `No API is named "Bright Data". datacircle_list_apis gives every API's name and slug.`},
		{SearchPlansName, `{"api":"bright-data","query":null}`, false, `these are the plans whose name holds \"bright data\".","plans":[{"provider":"Bright Data"`},
		{SearchPlansName, `{"query":"bright-data","sort":"price","limit":null}`, false, `"plans":[{"provider":"Bright Data"`},
		{ProviderName, `{"slug":"Bright Data"}`, false, `"apis":[{"api":"Employee email","best":{"plan":"A"`},
		{ProviderName, `{"slug":"bright-data","limit":1}`, false, `"ranked":1,"total":2`},
		{ProviderName, `{"slug":"bright-data","limit":1}`, false, `{"about":"Bright Data has plans on 2 APIs on Datacircle and a rank on 1 of them (1 is the best). Its best rank: 4, on Employee email.","apis":[`},
		{BestPlansName, `{"slug":"nope"}`, true, `no API has the slug nope. datacircle_list_apis gives every API's slug. A provider's plans: datacircle_search_plans`},
		{BestPlansName, `{"provider":"Bright Data","limit":2}`, true, `Give an API's slug, like email-finder: datacircle_list_apis gives every slug. A provider's plans:`},
		{ListAPIsName, `{"query":"Bright Data"}`, false, `"note":"No API's name holds every word of \"Bright Data\". A provider's plans: datacircle_search_plans`},
		{ProviderName, `{"slug":"down"}`, true, `datacircle: 502 Bad Gateway`},
		{BestPlansName, `{"slug":"email-finder","limit":[2]}`, true, `The arguments are no JSON object of this tool's parameters`},
		{ProviderName, `{"slug":"nope"}`, true, `no API has the slug nope. datacircle_search_plans with words of its name as query gives each plan's provider.`},
		{ListAPIsName, ``, false, `"total":2`},
		{ListAPIsName, `null`, false, `"total":2`},
		{"get_weather", `{}`, true, `datacircle: no tool is named "get_weather"`},
	} {
		got, err := tools.Run(ctx, call.name, call.arguments)
		if err != nil {
			got = err.Error()
		}
		if (err != nil) != call.fails || !strings.Contains(got, call.has) {
			t.Errorf("%s(%s) = %s, %v; want it to hold %s", call.name, call.arguments, got, err, call.has)
		}
		if strings.Contains(got, "terms") || strings.Contains(got, "sheetPage") {
			t.Errorf("%s gave a key it drops: %s", call.name, got)
		}
	}
	got, _ := tools.Run(ctx, ListAPIsName, `{"query":"employee email"}`)
	if strings.Contains(got, "note") {
		t.Errorf("datacircle_list_apis notes a query an API's name holds every word of: %s", got)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := tools.Run(canceled, ListAPIsName, `{}`); err != context.Canceled {
		t.Errorf("a canceled call gave %v", err)
	}
	if want := "/apis/email-finder/best?limit=2"; paths[2] != want {
		t.Errorf("called %s, want %s", paths[2], want)
	}
	if want := "/plans?query=bright+data&sort=price"; paths[11] != want {
		t.Errorf("called %s, want %s", paths[11], want)
	}

}

// fake answers as the API does, for the calls the tests below make.
func fake(t *testing.T) *httptest.Server {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/apis":
			w.Write([]byte(`[{"api":"Employee email","slug":"email-finder","plans":3}]`))
		case "/apis/email-finder/best":
			w.Write([]byte(`{"api":"Employee email","plans":[{"provider":"A","rank":1,"terms":"long"}]}`))
		default:
			w.WriteHeader(404)
			w.Write([]byte(`{"error":"no API has the slug nope"}`))
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func toolCall(id, name, arguments string) schemas.ChatAssistantMessageToolCall {
	return schemas.ChatAssistantMessageToolCall{ID: &id, Type: schemas.Ptr("function"),
		Function: schemas.ChatAssistantMessageToolCallFunction{Name: &name, Arguments: arguments}}
}

func TestExecute(t *testing.T) {
	tools := New(&Config{BaseURL: fake(t).URL})
	ctx := context.Background()
	for _, c := range []struct {
		call    schemas.ChatAssistantMessageToolCall
		content string
		isError bool
	}{
		{toolCall("call_1", BestPlansName, `{"slug":"email-finder","limit":null}`), `{"api":"Employee email","plans":[{"provider":"A","rank":1}]}`, false},
		{toolCall("call_2", BestPlansName, `{"slug":"nope"}`), `{"error":"no API has the slug nope. datacircle_list_apis gives every API's slug. A provider's plans: datacircle_search_plans with its name as query, like bright data, or datacircle_provider."}`, true},
		{toolCall("call_3", ListAPIsName, ``), `{"apis":[{"api":"Employee email","plans":3,"slug":"email-finder"}],"total":1}`, false},
	} {
		m, ok := tools.Execute(ctx, c.call)
		if !ok || m.Role != schemas.ChatMessageRoleTool || m.ChatToolMessage == nil || *m.ToolCallID != *c.call.ID ||
			*m.Content.ContentStr != c.content || *m.IsError != c.isError {
			t.Errorf("Execute(%s) = %+v", *c.call.ID, m)
		}
		// As Bifrost reads it back from JSON: a tool message with its call's id.
		sent, _ := json.Marshal(m)
		var back schemas.ChatMessage
		if err := json.Unmarshal(sent, &back); err != nil || back.ChatToolMessage == nil || *back.ToolCallID != *c.call.ID {
			t.Errorf("the message as JSON: %s (%v)", sent, err)
		}
	}
	if _, ok := tools.Execute(ctx, toolCall("call_4", "get_weather", `{}`)); ok {
		t.Errorf("Execute ran get_weather")
	}
	if _, ok := tools.Execute(ctx, schemas.ChatAssistantMessageToolCall{}); ok {
		t.Errorf("Execute ran a call with no name")
	}
}

// account serves no provider a call reaches in these tests, and the MCP clients it holds.
type account struct {
	clients map[string]*schemas.MCPClientConfig
}

func (a account) GetConfiguredProviders() ([]schemas.ModelProvider, error) {
	return []schemas.ModelProvider{schemas.OpenAI}, nil
}
func (a account) GetKeysForProvider(ctx context.Context, p schemas.ModelProvider) ([]schemas.Key, error) {
	return []schemas.Key{{Value: *schemas.NewSecretVar("unused"), Models: schemas.WhiteList{"*"}, Weight: 1}}, nil
}
func (a account) GetConfigForProvider(p schemas.ModelProvider) (*schemas.ProviderConfig, error) {
	return &schemas.ProviderConfig{NetworkConfig: schemas.DefaultNetworkConfig, ConcurrencyAndBufferSize: schemas.DefaultConcurrencyAndBufferSize}, nil
}
func (a account) GetConfiguredMCPClients() ([]string, error) {
	ids := []string{}
	for id := range a.clients {
		ids = append(ids, id)
	}
	return ids, nil
}
func (a account) GetConfigForMCPClient(id string) (*schemas.MCPClientConfig, error) {
	if c, ok := a.clients[id]; ok {
		return c, nil
	}
	return nil, fmt.Errorf("no MCP client %s", id)
}
func (a account) GetConfigForMCPClientByName(name string) (*schemas.MCPClientConfig, error) {
	return a.GetConfigForMCPClient(name)
}

func newBifrost(t *testing.T, clients map[string]*schemas.MCPClientConfig) *bifrost.Bifrost {
	client, err := bifrost.Init(context.Background(), schemas.BifrostConfig{Account: account{clients}, MCPConfig: &schemas.MCPConfig{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Shutdown)
	return client
}

func execute(t *testing.T, client *bifrost.Bifrost, name, arguments string) (string, bool) {
	m, berr := client.ExecuteChatMCPTool(schemas.NewBifrostContext(context.Background(), schemas.NoDeadline), schemas.Ptr(toolCall("call_9", name, arguments)))
	if berr != nil {
		t.Fatalf("ExecuteChatMCPTool(%s): %s", name, berr.Error.Message)
	}
	if m.Content == nil || m.Content.ContentStr == nil || m.ChatToolMessage == nil || *m.ToolCallID != "call_9" {
		t.Fatalf("ExecuteChatMCPTool(%s) = %+v", name, m)
	}
	return *m.Content.ContentStr, m.IsError != nil && *m.IsError
}

// listed gives the tools Bifrost lists for its MCP clients, by name.
func listed(t *testing.T, client *bifrost.Bifrost) map[string]schemas.MCPClientTool {
	clients, err := client.GetMCPClients()
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]schemas.MCPClientTool{}
	for _, c := range clients {
		for _, tool := range c.Tools {
			out[c.Config.Name+"-"+tool.Name] = tool
		}
	}
	return out
}

func TestRegister(t *testing.T) {
	client := newBifrost(t, nil)
	if err := New(&Config{BaseURL: fake(t).URL}).Register(client); err != nil {
		t.Fatal(err)
	}
	got, isError := execute(t, client, "bifrostInternal-"+BestPlansName, `{"slug":"email-finder"}`)
	if isError || got != `{"api":"Employee email","plans":[{"provider":"A","rank":1}]}` {
		t.Errorf("best plans: %s %v", got, isError)
	}
	got, isError = execute(t, client, "bifrostInternal-"+BestPlansName, `{"slug":"nope"}`)
	if !isError || !strings.HasPrefix(got, "Error: no API has the slug nope. datacircle_list_apis") {
		t.Errorf("a wrong slug: %s %v", got, isError)
	}
	t.Logf("Bifrost lists: %v", keys(listed(t, client)))
	if err := New(nil).Register(client); err == nil {
		t.Errorf("Register twice gave no error")
	}
}

func TestMCPClientConfig(t *testing.T) {
	tools := New(&Config{BaseURL: fake(t).URL})
	client := newBifrost(t, nil)
	if err := client.AddMCPClient(context.Background(), tools.MCPClientConfig()); err != nil {
		t.Fatal(err)
	}
	seen := listed(t, client)
	t.Logf("Bifrost lists: %v", keys(seen))
	if len(seen) != 4 {
		t.Fatalf("Bifrost lists %d tools, want 4: %v", len(seen), keys(seen))
	}
	sent, _ := json.Marshal(seen[ClientName+"-"+SearchPlansName])
	if !strings.Contains(string(sent), `"enum":["score","provider","price"]`) || !strings.Contains(string(sent), `"offset":{`) {
		t.Errorf("search plans as Bifrost lists it: %s", sent)
	}
	got, isError := execute(t, client, ClientName+"-"+BestPlansName, `{"slug":"email-finder","limit":"1"}`)
	if isError || got != `{"api":"Employee email","plans":[{"provider":"A","rank":1}]}` {
		t.Errorf("best plans: %s %v", got, isError)
	}
	got, isError = execute(t, client, ClientName+"-"+BestPlansName, `{"slug":"nope"}`)
	if !isError || !strings.Contains(got, "no API has the slug nope.") {
		t.Errorf("a wrong slug: %s %v", got, isError)
	}
}

func keys(m map[string]schemas.MCPClientTool) []string {
	out := []string{}
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestLive calls each tool once on the API itself, through Execute and through Bifrost's own MCP server:
// DATACIRCLE_LIVE=1 go test -run Live.
func TestLive(t *testing.T) {
	if os.Getenv("DATACIRCLE_LIVE") == "" {
		t.Skip("DATACIRCLE_LIVE is unset")
	}
	tools := New(nil)
	client := newBifrost(t, nil)
	if err := tools.Register(client); err != nil {
		t.Fatal(err)
	}
	for i, c := range []struct{ name, arguments, has string }{
		{ListAPIsName, `{"query":"email finder","limit":3}`, `"slug":"email-finder"`},
		{BestPlansName, `{"slug":"email-finder","limit":3}`, `"rank":1`},
		{SearchPlansName, `{"query":"bright data","sort":"price","limit":3}`, `"provider":"Bright Data"`},
		{ProviderName, `{"slug":"Bright Data","limit":3}`, `"about":"Bright Data has plans on`},
		{BestPlansName, `{"slug":"bright-data"}`, `no API has`},
	} {
		m, ok := tools.Execute(context.Background(), toolCall("call", c.name, c.arguments))
		if !ok || !strings.Contains(*m.Content.ContentStr, c.has) || *m.IsError != (i == 4) {
			t.Errorf("Execute %s(%s) = %.300s", c.name, c.arguments, *m.Content.ContentStr)
		}
		got, isError := execute(t, client, "bifrostInternal-"+c.name, c.arguments)
		if !strings.Contains(got, c.has) || isError != (i == 4) {
			t.Errorf("Bifrost's %s(%s) = %.300s", c.name, c.arguments, got)
		}
		t.Logf("%s(%s) = %.200s", c.name, c.arguments, got)
	}
}
