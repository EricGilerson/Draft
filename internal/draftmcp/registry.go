package draftmcp

import (
	"context"
	"fmt"
)

type toolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	Handler     toolHandler    `json:"-"`
}

type toolHandler func(context.Context, map[string]any) (any, error)

func allTools() []toolDef {
	tools := make([]toolDef, 0)
	tools = append(tools, metaTools()...)
	tools = append(tools, discoveryTools()...)
	tools = append(tools, serviceTools()...)
	tools = append(tools, envTools()...)
	tools = append(tools, environmentTools()...)
	tools = append(tools, sandboxTools()...)
	tools = append(tools, linksVolumesTools()...)
	tools = append(tools, dockerTools()...)
	tools = append(tools, importTools()...)
	tools = append(tools, settingsTools()...)
	return tools
}

func callTool(id any, name string, args map[string]any) rpcResponse {
	for _, tool := range allTools() {
		if tool.Name == name {
			value, err := tool.Handler(context.Background(), args)
			if err != nil {
				return toolErr(id, err)
			}
			return toolOK(id, value)
		}
	}
	return toolErrMsg(id, fmt.Sprintf("unknown tool: %s", name))
}

func tool(name, description string, properties map[string]any, required ...string) toolDef {
	schema := map[string]any{"type": "object", "properties": properties}
	if len(required) > 0 {
		schema["required"] = required
	}
	return toolDef{Name: name, Description: description, InputSchema: schema}
}

func withHandler(t toolDef, h toolHandler) toolDef {
	t.Handler = h
	return t
}

func stringsSchema(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func uintSchema(description string) map[string]any {
	return map[string]any{"type": "integer", "minimum": 1, "description": description}
}

func confirmSchema() map[string]any {
	return map[string]any{"type": "boolean", "description": "Must be true to perform this destructive operation."}
}
