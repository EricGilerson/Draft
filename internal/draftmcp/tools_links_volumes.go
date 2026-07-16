package draftmcp

import (
	"context"

	"Draft/internal/deploy"
)

func linksVolumesTools() []toolDef {
	return []toolDef{
		withHandler(tool("draft_linked_service_info", "Get linked-service information for an alias or root node.", map[string]any{"nodeId": stringsSchema("Node ID")}, "nodeId"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id, e := argString(args, "nodeId")
			if e != nil {
				return nil, e
			}
			return c.GetLinkedServiceInfo(ctx, id)
		}),
		withHandler(tool("draft_list_share_targets", "List environments/roots that can share this service.", map[string]any{"nodeId": stringsSchema("Node ID")}, "nodeId"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id, e := argString(args, "nodeId")
			if e != nil {
				return nil, e
			}
			return c.ListShareTargets(ctx, id)
		}),
		withHandler(tool("draft_list_shareable_roots", "List root services in other environments that can be shared into.", map[string]any{"projectId": uintSchema("Project ID"), "excludeEnvironmentId": uintSchema("Environment to exclude")}, "projectId", "excludeEnvironmentId"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			pid, e := argUint(args, "projectId")
			if e != nil {
				return nil, e
			}
			eid, e := argUint(args, "excludeEnvironmentId")
			if e != nil {
				return nil, e
			}
			return c.ListShareableRoots(ctx, pid, eid)
		}),
		withHandler(tool("draft_preview_link", "Preview converting a node into a shared alias of a root.", map[string]any{"nodeId": stringsSchema("Alias candidate node ID"), "rootNodeId": stringsSchema("Root node ID")}, "nodeId", "rootNodeId"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			n, e := argString(args, "nodeId")
			if e != nil {
				return nil, e
			}
			r, e := argString(args, "rootNodeId")
			if e != nil {
				return nil, e
			}
			return c.PreviewLinkToSharedRoot(ctx, n, r)
		}),
		withHandler(tool("draft_link_to_shared_root", "Link a node as an alias of a shared root. volumes: orphan|delete for this node's managed volumes.", map[string]any{"nodeId": stringsSchema("Node ID"), "rootNodeId": stringsSchema("Root node ID"), "volumes": stringsSchema("orphan or delete")}, "nodeId", "rootNodeId"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			n, e := argString(args, "nodeId")
			if e != nil {
				return nil, e
			}
			r, e := argString(args, "rootNodeId")
			if e != nil {
				return nil, e
			}
			vol := deploy.VolumeDisposition(optionalString(args, "volumes"))
			return map[string]string{"status": "linked"}, c.LinkToSharedRoot(ctx, n, r, vol)
		}),
		withHandler(tool("draft_promote_linked", "Promote a linked alias into a real service. seed: empty|clone; consistency: consistent|quick.", map[string]any{"nodeId": stringsSchema("Node ID"), "seed": stringsSchema("empty or clone"), "consistency": stringsSchema("consistent or quick"), "confirm": confirmSchema()}, "nodeId", "seed", "confirm"), func(ctx context.Context, args map[string]any) (any, error) {
			if e := requireConfirm(args); e != nil {
				return nil, e
			}
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			n, e := argString(args, "nodeId")
			if e != nil {
				return nil, e
			}
			seed, e := argString(args, "seed")
			if e != nil {
				return nil, e
			}
			cons := deploy.CloneConsistency(optionalString(args, "consistency"))
			return map[string]string{"status": "promoted"}, c.PromoteLinkedService(ctx, n, seed, cons)
		}),
		withHandler(tool("draft_unlink_service", "Unlink a shared alias. become describes what the node becomes after unlink.", map[string]any{"nodeId": stringsSchema("Node ID"), "become": stringsSchema("Become mode"), "confirm": confirmSchema()}, "nodeId", "become", "confirm"), func(ctx context.Context, args map[string]any) (any, error) {
			if e := requireConfirm(args); e != nil {
				return nil, e
			}
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			n, e := argString(args, "nodeId")
			if e != nil {
				return nil, e
			}
			b, e := argString(args, "become")
			if e != nil {
				return nil, e
			}
			return map[string]string{"status": "unlinked"}, c.UnlinkService(ctx, n, b)
		}),
		withHandler(tool("draft_volumes_overview", "Workspace overview of Draft-managed volumes (refcount/orphans).", map[string]any{}), func(ctx context.Context, _ map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			return c.ListVolumesOverview(ctx)
		}),
		withHandler(tool("draft_list_volumes", "List Draft-managed volumes for a project and/or node.", map[string]any{"projectId": uintSchema("Optional project ID"), "nodeId": stringsSchema("Optional node ID")}), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id := optionalUint(args, "projectId")
			if id == 0 {
				return c.ListManagedVolumes(ctx, nil, optionalString(args, "nodeId"))
			}
			return c.ListManagedVolumes(ctx, &id, optionalString(args, "nodeId"))
		}),
		withHandler(tool("draft_delete_volume", "Delete a Draft-managed volume.", map[string]any{"name": stringsSchema("Volume name"), "force": map[string]any{"type": "boolean"}, "confirm": confirmSchema()}, "name", "confirm"), func(ctx context.Context, args map[string]any) (any, error) {
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
			return map[string]string{"status": "deleted"}, c.DeleteManagedVolume(ctx, n, optionalBool(args, "force"))
		}),
		withHandler(tool("draft_preview_clone_volume", "Preview cloning volume data between services.", map[string]any{"targetNodeId": stringsSchema("Target node"), "sourceNodeId": stringsSchema("Source node"), "containerPath": stringsSchema("Container mount path")}, "targetNodeId", "sourceNodeId", "containerPath"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			t, e := argString(args, "targetNodeId")
			if e != nil {
				return nil, e
			}
			s, e := argString(args, "sourceNodeId")
			if e != nil {
				return nil, e
			}
			p, e := argString(args, "containerPath")
			if e != nil {
				return nil, e
			}
			return c.PreviewCloneVolume(ctx, t, s, p)
		}),
		withHandler(tool("draft_clone_volume", "Clone volume data between services. consistency: consistent|quick.", map[string]any{"targetNodeId": stringsSchema("Target node"), "sourceNodeId": stringsSchema("Source node"), "containerPath": stringsSchema("Container mount path"), "consistency": stringsSchema("consistent or quick"), "confirm": confirmSchema()}, "targetNodeId", "sourceNodeId", "containerPath", "confirm"), func(ctx context.Context, args map[string]any) (any, error) {
			if e := requireConfirm(args); e != nil {
				return nil, e
			}
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			t, e := argString(args, "targetNodeId")
			if e != nil {
				return nil, e
			}
			s, e := argString(args, "sourceNodeId")
			if e != nil {
				return nil, e
			}
			p, e := argString(args, "containerPath")
			if e != nil {
				return nil, e
			}
			cons := deploy.CloneConsistency(optionalString(args, "consistency"))
			if cons == "" {
				cons = deploy.CloneConsistent
			}
			return c.CloneVolumeData(ctx, t, s, p, cons)
		}),
	}
}