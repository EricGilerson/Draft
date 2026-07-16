package draftmcp

import (
	"context"
	"fmt"
)

func dockerTools() []toolDef {
	return []toolDef{
		withHandler(tool("draft_run_command", "Run a one-shot command in a service's running container. cmd is an argv array, not a shell string.", map[string]any{"nodeId": stringsSchema("Node ID"), "cmd": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, "workDir": stringsSchema("Optional working directory")}, "nodeId", "cmd"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			n, e := argString(args, "nodeId")
			if e != nil {
				return nil, e
			}
			raw, ok := args["cmd"].([]any)
			if !ok {
				return nil, fmt.Errorf("cmd must be an array")
			}
			cmd := make([]string, len(raw))
			for i, v := range raw {
				var ok bool
				cmd[i], ok = v.(string)
				if !ok {
					return nil, fmt.Errorf("cmd values must be strings")
				}
			}
			return c.RunCommand(ctx, n, cmd, optionalString(args, "workDir"))
		}),
		withHandler(tool("draft_docker_containers", "List Docker containers.", map[string]any{}), func(ctx context.Context, _ map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			return c.ListContainers(ctx)
		}),
		withHandler(tool("draft_docker_container_start", "Start a Docker container by ID.", map[string]any{"id": stringsSchema("Container ID")}, "id"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id, e := argString(args, "id")
			if e != nil {
				return nil, e
			}
			return map[string]string{"status": "started"}, c.StartContainer(ctx, id)
		}),
		withHandler(tool("draft_docker_container_stop", "Stop a Docker container by ID.", map[string]any{"id": stringsSchema("Container ID")}, "id"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id, e := argString(args, "id")
			if e != nil {
				return nil, e
			}
			return map[string]string{"status": "stopped"}, c.StopContainer(ctx, id)
		}),
		withHandler(tool("draft_docker_container_restart", "Restart a Docker container by ID.", map[string]any{"id": stringsSchema("Container ID")}, "id"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id, e := argString(args, "id")
			if e != nil {
				return nil, e
			}
			return map[string]string{"status": "restarted"}, c.RestartContainer(ctx, id)
		}),
		withHandler(tool("draft_docker_container_remove", "Remove a Docker container.", map[string]any{"id": stringsSchema("Container ID"), "force": map[string]any{"type": "boolean"}, "confirm": confirmSchema()}, "id", "confirm"), func(ctx context.Context, args map[string]any) (any, error) {
			if e := requireConfirm(args); e != nil {
				return nil, e
			}
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id, e := argString(args, "id")
			if e != nil {
				return nil, e
			}
			return map[string]string{"status": "removed"}, c.RemoveContainer(ctx, id, optionalBool(args, "force"))
		}),
		withHandler(tool("draft_docker_images", "List Docker images.", map[string]any{}), func(ctx context.Context, _ map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			return c.ListImages(ctx)
		}),
		withHandler(tool("draft_docker_image_remove", "Remove a Docker image.", map[string]any{"id": stringsSchema("Image ID"), "force": map[string]any{"type": "boolean"}, "confirm": confirmSchema()}, "id", "confirm"), func(ctx context.Context, args map[string]any) (any, error) {
			if e := requireConfirm(args); e != nil {
				return nil, e
			}
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id, e := argString(args, "id")
			if e != nil {
				return nil, e
			}
			return map[string]string{"status": "removed"}, c.RemoveImage(ctx, id, optionalBool(args, "force"))
		}),
		withHandler(tool("draft_docker_networks", "List Docker networks.", map[string]any{}), func(ctx context.Context, _ map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			return c.ListNetworks(ctx)
		}),
		withHandler(tool("draft_docker_network_remove", "Remove a Docker network.", map[string]any{"id": stringsSchema("Network ID"), "confirm": confirmSchema()}, "id", "confirm"), func(ctx context.Context, args map[string]any) (any, error) {
			if e := requireConfirm(args); e != nil {
				return nil, e
			}
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id, e := argString(args, "id")
			if e != nil {
				return nil, e
			}
			return map[string]string{"status": "removed"}, c.RemoveNetwork(ctx, id)
		}),
		withHandler(tool("draft_docker_volumes_all", "List all Docker volumes (not only Draft-managed).", map[string]any{}), func(ctx context.Context, _ map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			return c.ListAllVolumes(ctx)
		}),
		withHandler(tool("draft_docker_volume_remove", "Remove any Docker volume by name.", map[string]any{"name": stringsSchema("Volume name"), "force": map[string]any{"type": "boolean"}, "confirm": confirmSchema()}, "name", "confirm"), func(ctx context.Context, args map[string]any) (any, error) {
			if e := requireConfirm(args); e != nil {
				return nil, e
			}
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			n, e := argString(args, "name")
			if e != nil {
				return nil, e
			}
			return map[string]string{"status": "removed"}, c.RemoveVolume(ctx, n, optionalBool(args, "force"))
		}),
		withHandler(tool("draft_docker_prune", "Prune Docker resources. resource: containers|images|networks|volumes|buildcache.", map[string]any{"resource": stringsSchema("Resource type"), "draftOnly": map[string]any{"type": "boolean"}, "confirm": confirmSchema()}, "resource", "confirm"), func(ctx context.Context, args map[string]any) (any, error) {
			if e := requireConfirm(args); e != nil {
				return nil, e
			}
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			r, e := argString(args, "resource")
			if e != nil {
				return nil, e
			}
			return c.PruneDocker(ctx, r, optionalBool(args, "draftOnly"))
		}),
		withHandler(tool("draft_docker_disk_usage", "Get Docker disk usage.", map[string]any{}), func(ctx context.Context, _ map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			return c.SystemDF(ctx)
		}),
		withHandler(tool("draft_delete_project", "Delete a project and all of its services.", map[string]any{"projectId": uintSchema("Project ID"), "confirm": confirmSchema()}, "projectId", "confirm"), func(ctx context.Context, args map[string]any) (any, error) {
			if e := requireConfirm(args); e != nil {
				return nil, e
			}
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id, e := argUint(args, "projectId")
			if e != nil {
				return nil, e
			}
			return map[string]string{"status": "deleted"}, c.DeleteProject(ctx, id)
		}),
	}
}