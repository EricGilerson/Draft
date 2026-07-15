package draftpack

import (
	"math"
	"strings"

	"Draft/internal/store"
)

const (
	// Approximate service-node footprint used for overlap avoidance.
	nodeWidth  = 220.0
	nodeHeight = 100.0
	gridGapX   = 40.0
	gridGapY   = 40.0
	// Extra padding when shifting a pack group away from existing nodes.
	packPad = 48.0
)

// layoutPlan assigns final canvas coordinates for selected services.
func (e *Importer) layoutPlan(pack *Pack, services []ServicePayload, opts ImportOptions, envIDByKey map[string]uint) map[string][2]float64 {
	mode := strings.TrimSpace(opts.LayoutMode)
	if mode == "" {
		mode = LayoutAuto
	}

	out := map[string][2]float64{}
	if len(services) == 0 {
		return out
	}

	hasLayout := false
	for _, svc := range services {
		if svc.X != 0 || svc.Y != 0 {
			hasLayout = true
			break
		}
	}

	// Preserve: raw pack coords (zeros stay zeros).
	if mode == LayoutPreserve {
		for _, svc := range services {
			out[svc.Key] = [2]float64{svc.X, svc.Y}
		}
		return out
	}

	// Grid: always free grid (ignore pack coords).
	if mode == LayoutGrid || !hasLayout {
		// Group by destination environment so multi-env recreate gets independent grids.
		byEnv := map[uint][]ServicePayload{}
		for _, svc := range services {
			envID := envIDByKey[svc.EnvironmentKey]
			if envID == 0 {
				// Fallbacks already applied by caller; use first env id if any.
				for _, id := range envIDByKey {
					envID = id
					break
				}
			}
			byEnv[envID] = append(byEnv[envID], svc)
		}
		for envID, group := range byEnv {
			originX, originY := e.freeOrigin(envID, opts)
			placeGrid(group, originX, originY, out)
		}
		return out
	}

	// Auto with layout: keep relative pack positions; shift whole group per destination env
	// so the bounding box does not heavily overlap existing nodes.
	byEnv := map[uint][]ServicePayload{}
	for _, svc := range services {
		envID := envIDByKey[svc.EnvironmentKey]
		if envID == 0 {
			for _, id := range envIDByKey {
				envID = id
				break
			}
		}
		byEnv[envID] = append(byEnv[envID], svc)
	}
	for envID, group := range byEnv {
		minX, minY := math.Inf(1), math.Inf(1)
		maxX, maxY := math.Inf(-1), math.Inf(-1)
		for _, svc := range group {
			if svc.X < minX {
				minX = svc.X
			}
			if svc.Y < minY {
				minY = svc.Y
			}
			if svc.X > maxX {
				maxX = svc.X
			}
			if svc.Y > maxY {
				maxY = svc.Y
			}
		}
		if math.IsInf(minX, 1) {
			continue
		}
		// Normalize so pack-relative layout is preserved from (0,0) of the group.
		// Then decide whether to shift the whole group.
		dx, dy := 0.0, 0.0
		if e.bboxOverlapsExisting(envID, minX, minY, maxX+nodeWidth, maxY+nodeHeight) {
			ox, oy := e.freeOrigin(envID, opts)
			dx = ox - minX
			dy = oy - minY
		}
		for _, svc := range group {
			out[svc.Key] = [2]float64{svc.X + dx, svc.Y + dy}
		}
	}
	return out
}

func placeGrid(group []ServicePayload, originX, originY float64, out map[string][2]float64) {
	cols := int(math.Ceil(math.Sqrt(float64(len(group)))))
	if cols < 1 {
		cols = 1
	}
	for i, svc := range group {
		col := i % cols
		row := i / cols
		out[svc.Key] = [2]float64{
			originX + float64(col)*(nodeWidth+gridGapX),
			originY + float64(row)*(nodeHeight+gridGapY),
		}
	}
}

func (e *Importer) freeOrigin(environmentID uint, opts ImportOptions) (float64, float64) {
	if environmentID == 0 {
		return 80, 80
	}
	nodes, err := e.Store.ListNodesByEnvironment(environmentID)
	if err != nil || len(nodes) == 0 {
		return 80, 80
	}
	maxX, maxY := 0.0, 0.0
	for _, n := range nodes {
		if n.X+nodeWidth > maxX {
			maxX = n.X + nodeWidth
		}
		if n.Y+nodeHeight > maxY {
			maxY = n.Y + nodeHeight
		}
	}
	// Prefer to the right of the densest area; if very wide, stack below.
	if maxX > 1600 {
		return 80, maxY + packPad
	}
	return maxX + packPad, 80
}

func (e *Importer) bboxOverlapsExisting(environmentID uint, minX, minY, maxX, maxY float64) bool {
	if environmentID == 0 {
		return false
	}
	nodes, err := e.Store.ListNodesByEnvironment(environmentID)
	if err != nil || len(nodes) == 0 {
		return false
	}
	for _, n := range nodes {
		nx1, ny1 := n.X, n.Y
		nx2, ny2 := n.X+nodeWidth, n.Y+nodeHeight
		if minX < nx2 && maxX > nx1 && minY < ny2 && maxY > ny1 {
			return true
		}
	}
	return false
}

// selectedServices returns pack services filtered by ServiceKeys (empty = all).
func selectedServices(pack *Pack, keys []string) []ServicePayload {
	if pack == nil {
		return nil
	}
	if len(keys) == 0 {
		return pack.Services
	}
	want := map[string]bool{}
	for _, k := range keys {
		k = strings.TrimSpace(k)
		if k != "" {
			want[k] = true
		}
	}
	var out []ServicePayload
	for _, svc := range pack.Services {
		if want[svc.Key] {
			out = append(out, svc)
		}
	}
	return out
}

// buildLayoutPreview computes proposed placement for the UI mini-map.
// Mirrors import's env mapping (flatten vs recreate) and layoutPlan.
func (e *Importer) buildLayoutPreview(pack *Pack, services []ServicePayload, opts PreviewOptions) *LayoutPreview {
	layoutMode := strings.TrimSpace(opts.LayoutMode)
	if layoutMode == "" {
		layoutMode = LayoutAuto
	}
	mode := opts.Mode
	if mode == "" {
		mode = ImportAsNewProject
	}
	envMode := strings.TrimSpace(opts.EnvImportMode)
	if envMode == "" {
		envMode = EnvImportFlatten
	}

	lp := &LayoutPreview{
		Mode:       layoutMode,
		NodeWidth:  nodeWidth,
		NodeHeight: nodeHeight,
	}
	if len(services) == 0 {
		return lp
	}

	// Resolve destination env IDs the same way import will (best-effort for preview).
	envIDByKey := map[string]uint{}
	var existingNodes []store.CanvasNode

	if mode == ImportIntoProject && opts.ProjectID != 0 {
		if envMode == EnvImportRecreate && len(pack.Environments) > 0 {
			existing, _ := e.Store.ListEnvironments(opts.ProjectID)
			byName := map[string]store.Environment{}
			for _, env := range existing {
				byName[strings.ToLower(strings.TrimSpace(env.Name))] = env
			}
			for _, pe := range pack.Environments {
				name := pe.Name
				if opts.EnvironmentNameOverrides != nil {
					if o := strings.TrimSpace(opts.EnvironmentNameOverrides[pe.Key]); o != "" {
						name = o
					}
				}
				if name == "" {
					name = pe.Key
				}
				if env, ok := byName[strings.ToLower(strings.TrimSpace(name))]; ok {
					envIDByKey[pe.Key] = env.ID
				}
			}
			// Collect existing nodes from mapped envs only (new envs are empty).
			seen := map[uint]bool{}
			for _, id := range envIDByKey {
				if seen[id] {
					continue
				}
				seen[id] = true
				nodes, _ := e.Store.ListNodesByEnvironment(id)
				existingNodes = append(existingNodes, nodes...)
			}
		} else if opts.EnvironmentID != 0 {
			for _, pe := range pack.Environments {
				envIDByKey[pe.Key] = opts.EnvironmentID
			}
			if len(envIDByKey) == 0 {
				envIDByKey["default"] = opts.EnvironmentID
			}
			for _, svc := range services {
				if _, ok := envIDByKey[svc.EnvironmentKey]; !ok {
					envIDByKey[svc.EnvironmentKey] = opts.EnvironmentID
				}
			}
			nodes, _ := e.Store.ListNodesByEnvironment(opts.EnvironmentID)
			existingNodes = nodes
		}
	}

	importOpts := ImportOptions{
		Mode:          mode,
		ProjectID:     opts.ProjectID,
		EnvironmentID: opts.EnvironmentID,
		EnvImportMode: envMode,
		LayoutMode:    layoutMode,
		ServiceKeys:   opts.ServiceKeys,
	}
	coords := e.layoutPlan(pack, services, importOpts, envIDByKey)

	// Detect whether raw pack coords would overlap (for "would stack" warning).
	if len(existingNodes) > 0 {
		for _, svc := range services {
			if rectOverlapsAny(svc.X, svc.Y, existingNodes, nil) {
				lp.WouldOverlapWithoutShift = true
				break
			}
		}
	}
	// Also pack-internal stacking at identical coords with no layout.
	if !lp.WouldOverlapWithoutShift {
		for i := 0; i < len(services); i++ {
			for j := i + 1; j < len(services); j++ {
				if rectsOverlap(services[i].X, services[i].Y, services[j].X, services[j].Y) {
					// Only count as "would stack" when both are at same/overlapping pack positions
					// and layout wouldn't separate them in preserve mode.
					if layoutMode == LayoutPreserve {
						lp.WouldOverlapWithoutShift = true
					}
					break
				}
			}
		}
	}

	for _, n := range existingNodes {
		lp.Existing = append(lp.Existing, LayoutNode{
			Key:   n.ID,
			Label: n.Label,
			X:     n.X,
			Y:     n.Y,
			Kind:  "existing",
		})
	}

	effectiveLabel := func(svc ServicePayload) string {
		if opts.ServiceLabelOverrides != nil {
			if o := strings.TrimSpace(opts.ServiceLabelOverrides[svc.Key]); o != "" {
				return o
			}
		}
		return svc.Label
	}

	for _, svc := range services {
		xy := coords[svc.Key]
		node := LayoutNode{
			Key:            svc.Key,
			Label:          effectiveLabel(svc),
			X:              xy[0],
			Y:              xy[1],
			PackX:          svc.X,
			PackY:          svc.Y,
			Kind:           "incoming",
			EnvironmentKey: svc.EnvironmentKey,
		}
		if xy[0] != svc.X || xy[1] != svc.Y {
			lp.Shifted = true
		}
		// Overlap against existing
		var labels []string
		for _, ex := range existingNodes {
			if rectsOverlap(xy[0], xy[1], ex.X, ex.Y) {
				node.Overlaps = true
				labels = append(labels, ex.Label)
			}
		}
		// Overlap against other incoming (already placed)
		for _, other := range lp.Incoming {
			if rectsOverlap(xy[0], xy[1], other.X, other.Y) {
				node.Overlaps = true
				labels = append(labels, other.Label)
			}
		}
		node.OverlapsLabels = labels
		if node.Overlaps {
			lp.OverlapCount++
		}
		lp.Incoming = append(lp.Incoming, node)
	}

	// Bounds
	lp.MinX, lp.MinY = math.Inf(1), math.Inf(1)
	lp.MaxX, lp.MaxY = math.Inf(-1), math.Inf(-1)
	expand := func(x, y float64) {
		if x < lp.MinX {
			lp.MinX = x
		}
		if y < lp.MinY {
			lp.MinY = y
		}
		if x+nodeWidth > lp.MaxX {
			lp.MaxX = x + nodeWidth
		}
		if y+nodeHeight > lp.MaxY {
			lp.MaxY = y + nodeHeight
		}
	}
	for _, n := range lp.Existing {
		expand(n.X, n.Y)
	}
	for _, n := range lp.Incoming {
		expand(n.X, n.Y)
	}
	if math.IsInf(lp.MinX, 1) {
		lp.MinX, lp.MinY, lp.MaxX, lp.MaxY = 0, 0, 400, 300
	}
	// Padding for mini-map breathing room
	pad := 40.0
	lp.MinX -= pad
	lp.MinY -= pad
	lp.MaxX += pad
	lp.MaxY += pad

	return lp
}

func rectsOverlap(ax, ay, bx, by float64) bool {
	return ax < bx+nodeWidth && ax+nodeWidth > bx && ay < by+nodeHeight && ay+nodeHeight > by
}

func rectOverlapsAny(x, y float64, existing []store.CanvasNode, extra []LayoutNode) bool {
	for _, n := range existing {
		if rectsOverlap(x, y, n.X, n.Y) {
			return true
		}
	}
	for _, n := range extra {
		if rectsOverlap(x, y, n.X, n.Y) {
			return true
		}
	}
	return false
}
