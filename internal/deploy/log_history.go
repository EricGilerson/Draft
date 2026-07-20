package deploy

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"sort"
	"strconv"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

const maxContainerLogHistoryTail = 10000

// GetContainerLogHistory returns a non-following Docker log snapshot. The
// requested tail is capped so a scroll gesture cannot accidentally pull an
// unbounded container log into the desktop renderer.
func (e *Engine) GetContainerLogHistory(ctx context.Context, nodeID string, tail int) (*LogHistory, error) {
	if tail < 1 || tail > maxContainerLogHistoryTail {
		return nil, fmt.Errorf("log history tail must be between 1 and %d", maxContainerLogHistoryTail)
	}

	dep, err := e.store.ActiveDeployment(nodeID)
	if err != nil || dep == nil || dep.ContainerID == "" {
		return nil, fmt.Errorf("no active container for log history")
	}
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	defer cli.Close()

	reader, err := cli.ContainerLogs(ctx, dep.ContainerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Tail:       strconv.Itoa(tail),
		Timestamps: true,
	})
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	var stdout, stderr bytes.Buffer
	if _, err := stdcopy.StdCopy(&stdout, &stderr, reader); err != nil {
		return nil, err
	}
	lines := append(scanContainerLogLines(stdout.Bytes(), "stdout"), scanContainerLogLines(stderr.Bytes(), "stderr")...)
	sort.SliceStable(lines, func(i, j int) bool { return lines[i].Timestamp < lines[j].Timestamp })
	return &LogHistory{Lines: lines, HasMore: len(lines) >= tail}, nil
}

func scanContainerLogLines(data []byte, stream string) []LogLine {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var lines []LogLine
	for scanner.Scan() {
		lines = append(lines, parseContainerLogLine(scanner.Text(), stream))
	}
	return lines
}
