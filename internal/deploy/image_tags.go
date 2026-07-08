package deploy

import (
	"context"
	"strings"

	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/docker/docker/errdefs"
)

// previousTagSuffix marks a Draft-built image kept as the N-1 rollback
// candidate. Live deploys use draftImageTag (…:N); after cutover the previous
// build is retagged to …:N-previous so the Docker tab still names it clearly
// instead of leaving an anonymous untagged layer. N-2 and older drop both the
// live and -previous names (see stopPrevious).
const previousTagSuffix = "-previous"

// draftPreviousImageTag returns the rollback retention tag for a live Draft
// image tag.
//
//	draft-myapp-main-api:3  →  draft-myapp-main-api:3-previous
func draftPreviousImageTag(liveTag string) string {
	liveTag = strings.TrimSpace(liveTag)
	if liveTag == "" {
		return ""
	}
	if strings.HasSuffix(liveTag, previousTagSuffix) {
		return liveTag
	}
	return liveTag + previousTagSuffix
}

// isDraftPreviousImageTag reports whether tag is a Draft rollback retention tag.
func isDraftPreviousImageTag(tag string) bool {
	return strings.HasSuffix(strings.TrimSpace(tag), previousTagSuffix)
}

// resolveLocalDraftImageRef finds a local Docker reference for a deployment's
// stored ImageTag. After cutover the live name may have been moved to the
// -previous form, so we check both.
func (e *Engine) resolveLocalDraftImageRef(ctx context.Context, cli *client.Client, imageTag string) (string, bool) {
	imageTag = strings.TrimSpace(imageTag)
	if imageTag == "" || cli == nil {
		return "", false
	}
	if e.imageExistsLocally(ctx, cli, imageTag) {
		return imageTag, true
	}
	prev := draftPreviousImageTag(imageTag)
	if prev != imageTag && e.imageExistsLocally(ctx, cli, prev) {
		return prev, true
	}
	return "", false
}

// retainImageForRollback keeps a just-superseded build identifiable for
// rollback: ensure it is tagged …:N-previous and drop the live …:N name so the
// Docker tab shows a clear "previous" tag rather than an untagged image (or a
// second live-looking name). No-op when the image is already missing.
func retainImageForRollback(ctx context.Context, cli *client.Client, liveTag string) error {
	liveTag = strings.TrimSpace(liveTag)
	if liveTag == "" || cli == nil {
		return nil
	}
	prev := draftPreviousImageTag(liveTag)

	// Already only under the previous name.
	if !imageRefExists(ctx, cli, liveTag) {
		return nil
	}

	if prev != liveTag {
		if err := cli.ImageTag(ctx, liveTag, prev); err != nil {
			return err
		}
		// Untag the live name only. ImageRemove by tag removes that name when
		// another tag still points at the same ID — the -previous tag keeps
		// the image alive and labeled for rollback.
		_, err := cli.ImageRemove(ctx, liveTag, image.RemoveOptions{})
		if err != nil && !errdefs.IsNotFound(err) && !errdefs.IsConflict(err) {
			// Conflict usually means a container still references the live
			// name; the -previous tag is already in place so identity is fine.
			return nil
		}
	}
	return nil
}

// removeDraftDeploymentImage deletes a deployment's Draft-built image under
// both its live and -previous tags. Used when the image is no longer the N-1
// rollback candidate (N-2+), on keep_images=none, service delete, and stop.
func removeDraftDeploymentImage(ctx context.Context, cli *client.Client, liveTag string) error {
	liveTag = strings.TrimSpace(liveTag)
	if liveTag == "" || cli == nil {
		return nil
	}
	prev := draftPreviousImageTag(liveTag)
	var firstErr error
	if prev != liveTag {
		if err := removeImageAndWait(ctx, cli, prev); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if err := removeImageAndWait(ctx, cli, liveTag); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

func imageRefExists(ctx context.Context, cli *client.Client, ref string) bool {
	if ref == "" || cli == nil {
		return false
	}
	_, _, err := cli.ImageInspectWithRaw(ctx, ref)
	return err == nil
}
