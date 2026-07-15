package deploy

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"Draft/internal/draftpack"
)

// Re-export draftpack types for Wails / daemon JSON surface.
type (
	DraftPackExportOptions  = draftpack.ExportOptions
	DraftPackImportOptions  = draftpack.ImportOptions
	DraftPackPreviewOptions = draftpack.PreviewOptions
	DraftPackExportResult   = draftpack.ExportResult
	DraftPackImportPreview  = draftpack.ImportPreview
	DraftPackImportResult   = draftpack.ImportResult
	DraftPack               = draftpack.Pack
)

// DefaultDraftPackExportOptions returns safe cross-machine export defaults.
func DefaultDraftPackExportOptions() DraftPackExportOptions {
	return draftpack.DefaultExportOptions()
}

// ExportDraftPackService builds a Draft pack for one service.
func (e *Engine) ExportDraftPackService(nodeID string, opts DraftPackExportOptions) (*DraftPackExportResult, error) {
	ex := &draftpack.Exporter{Store: e.store}
	pack, err := ex.ExportService(nodeID, mergeExportOpts(opts))
	if err != nil {
		return nil, err
	}
	return packResult(pack)
}

// ExportDraftPackEnvironment builds a Draft pack for one environment.
func (e *Engine) ExportDraftPackEnvironment(environmentID uint, opts DraftPackExportOptions) (*DraftPackExportResult, error) {
	ex := &draftpack.Exporter{Store: e.store}
	pack, err := ex.ExportEnvironment(environmentID, mergeExportOpts(opts))
	if err != nil {
		return nil, err
	}
	return packResult(pack)
}

// ExportDraftPackProject builds a Draft pack for a project.
func (e *Engine) ExportDraftPackProject(projectID uint, opts DraftPackExportOptions) (*DraftPackExportResult, error) {
	ex := &draftpack.Exporter{Store: e.store}
	pack, err := ex.ExportProject(projectID, mergeExportOpts(opts))
	if err != nil {
		return nil, err
	}
	return packResult(pack)
}

// ExportDraftPackToPath builds a pack and writes it to destPath (file path).
// If destPath is a directory, a suggested filename is used inside it.
func (e *Engine) ExportDraftPackToPath(scope string, id string, projectID, environmentID uint, opts DraftPackExportOptions, destPath string) (*DraftPackExportResult, error) {
	var res *DraftPackExportResult
	var err error
	switch strings.TrimSpace(scope) {
	case draftpack.ScopeService:
		res, err = e.ExportDraftPackService(id, opts)
	case draftpack.ScopeEnvironment:
		res, err = e.ExportDraftPackEnvironment(environmentID, opts)
	case draftpack.ScopeProject:
		res, err = e.ExportDraftPackProject(projectID, opts)
	default:
		return nil, fmt.Errorf("unknown draft pack scope %q", scope)
	}
	if err != nil {
		return nil, err
	}
	path, err := writePackFile(destPath, res.FileName, []byte(res.JSON))
	if err != nil {
		return nil, err
	}
	res.Path = path
	return res, nil
}

// PreviewDraftPackImport parses a .draftpack file and returns a dry-run preview.
// opts scopes collision detection (mode, target project/env, proposed renames).
func (e *Engine) PreviewDraftPackImport(path string, opts draftpack.PreviewOptions) (*DraftPackImportPreview, error) {
	pack, err := readPackFile(path)
	if err != nil {
		return nil, err
	}
	imp := &draftpack.Importer{Store: e.store}
	return imp.Preview(pack, opts)
}

// ImportDraftPack applies a pack from path with the given options.
func (e *Engine) ImportDraftPack(path string, opts DraftPackImportOptions) (*DraftPackImportResult, error) {
	pack, err := readPackFile(path)
	if err != nil {
		return nil, err
	}
	imp := &draftpack.Importer{Store: e.store}
	return imp.Import(pack, opts)
}

// PreviewDraftPackJSON parses pack JSON (clipboard paste / no file) and returns a dry-run preview.
func (e *Engine) PreviewDraftPackJSON(data []byte, opts draftpack.PreviewOptions) (*DraftPackImportPreview, error) {
	pack, err := draftpack.UnmarshalPack(data)
	if err != nil {
		return nil, err
	}
	imp := &draftpack.Importer{Store: e.store}
	return imp.Preview(pack, opts)
}

// ImportDraftPackJSON applies pack JSON (clipboard paste / no file).
func (e *Engine) ImportDraftPackJSON(data []byte, opts DraftPackImportOptions) (*DraftPackImportResult, error) {
	pack, err := draftpack.UnmarshalPack(data)
	if err != nil {
		return nil, err
	}
	imp := &draftpack.Importer{Store: e.store}
	return imp.Import(pack, opts)
}

func mergeExportOpts(opts DraftPackExportOptions) DraftPackExportOptions {
	// If caller sent a fully zero struct, use defaults. Otherwise keep as-is
	// (frontend always sends full options from Default + toggles).
	d := draftpack.DefaultExportOptions()
	if !opts.IncludeSecretValues && !opts.IncludeAppSecrets && !opts.IncludeBindHostPaths &&
		!opts.IncludeServiceRoots && !opts.IncludeProjectEnvVars && !opts.IncludeSandboxProfiles &&
		!opts.IncludeCanvasLayout && !opts.IncludeGitSettings && !opts.IncludeSourceConfig &&
		len(opts.EnvironmentIDs) == 0 {
		return d
	}
	return opts
}

func packResult(pack *draftpack.Pack) (*DraftPackExportResult, error) {
	raw, err := draftpack.MarshalPack(pack)
	if err != nil {
		return nil, err
	}
	return &DraftPackExportResult{
		Pack:     pack,
		JSON:     string(raw),
		FileName: draftpack.SuggestedFileName(pack),
		Report:   pack.Report,
	}, nil
}

func readPackFile(path string) (*draftpack.Pack, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read pack: %w", err)
	}
	return draftpack.UnmarshalPack(data)
}

func writePackFile(destPath, fileName string, data []byte) (string, error) {
	destPath = strings.TrimSpace(destPath)
	if destPath == "" {
		return "", fmt.Errorf("destination path is required")
	}
	info, err := os.Stat(destPath)
	target := destPath
	if err == nil && info.IsDir() {
		if fileName == "" {
			fileName = "pack.draftpack"
		}
		target = filepath.Join(destPath, fileName)
	} else if strings.HasSuffix(strings.ToLower(destPath), string(filepath.Separator)) {
		if fileName == "" {
			fileName = "pack.draftpack"
		}
		target = filepath.Join(destPath, fileName)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(target, data, 0o644); err != nil {
		return "", fmt.Errorf("write pack: %w", err)
	}
	return target, nil
}
