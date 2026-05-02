// Package docker wraps the Docker SDK for Go.
// All Docker operations in Trangly go through this package.
// No other package may call the Docker API or exec "docker ..." commands directly.
package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
)

// StatSample holds a single RSS measurement from docker stats.
type StatSample struct {
	ContainerID string
	RSSMB       int64
	Timestamp   time.Time
}

// Client wraps the Docker SDK client.
type Client struct {
	c *client.Client
}

// New creates a Client connected to the local Docker daemon via the default socket.
func New() (*Client, error) {
	c, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("docker: new client: %w", err)
	}
	return &Client{c: c}, nil
}

// Close releases the underlying HTTP transport.
func (cl *Client) Close() error {
	return cl.c.Close()
}

// Ping checks that the Docker daemon is reachable.
func (cl *Client) Ping(ctx context.Context) error {
	_, err := cl.c.Ping(ctx)
	return err
}

// ImageExists returns true if an image with the given tag is present locally.
func (cl *Client) ImageExists(ctx context.Context, tag string) (bool, error) {
	_, _, err := cl.c.ImageInspectWithRaw(ctx, tag)
	if client.IsErrNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("docker: inspect image %s: %w", tag, err)
	}
	return true, nil
}

// ImageTag adds a new tag to an existing image (src → dst).
func (cl *Client) ImageTag(ctx context.Context, src, dst string) error {
	if err := cl.c.ImageTag(ctx, src, dst); err != nil {
		return fmt.Errorf("docker: tag %s → %s: %w", src, dst, err)
	}
	return nil
}

// ContainerStop stops a running container by name. Missing containers are silently ignored.
func (cl *Client) ContainerStop(ctx context.Context, name string) error {
	timeout := 30 // seconds
	err := cl.c.ContainerStop(ctx, name, container.StopOptions{Timeout: &timeout})
	if client.IsErrNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("docker: stop container %s: %w", name, err)
	}
	return nil
}

// ContainerRemove removes a container by name. Missing containers are silently ignored.
func (cl *Client) ContainerRemove(ctx context.Context, name string) error {
	err := cl.c.ContainerRemove(ctx, name, container.RemoveOptions{Force: true})
	if client.IsErrNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("docker: remove container %s: %w", name, err)
	}
	return nil
}

// ContainerCreate creates (but does not start) a container.
func (cl *Client) ContainerCreate(ctx context.Context, cfg *container.Config, hostCfg *container.HostConfig, name string) (string, error) {
	resp, err := cl.c.ContainerCreate(ctx, cfg, hostCfg, nil, nil, name)
	if err != nil {
		return "", fmt.Errorf("docker: create container %s: %w", name, err)
	}
	return resp.ID, nil
}

// ContainerStart starts a created container.
func (cl *Client) ContainerStart(ctx context.Context, containerID string) error {
	if err := cl.c.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
		return fmt.Errorf("docker: start container %s: %w", containerID, err)
	}
	return nil
}

// ContainerInspect returns the current state of a container.
func (cl *Client) ContainerInspect(ctx context.Context, nameOrID string) (types.ContainerJSON, error) {
	info, err := cl.c.ContainerInspect(ctx, nameOrID)
	if err != nil {
		return types.ContainerJSON{}, fmt.Errorf("docker: inspect container %s: %w", nameOrID, err)
	}
	return info, nil
}

// Stats samples the RSS of a running container every interval until ctx is done.
// The returned channel is closed when sampling stops.
func (cl *Client) Stats(ctx context.Context, containerID string, interval time.Duration) (<-chan StatSample, error) {
	ch := make(chan StatSample, 16)
	go func() {
		defer close(ch)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				sample, err := cl.singleStatSample(ctx, containerID)
				if err != nil {
					return
				}
				select {
				case ch <- sample:
				default:
				}
			}
		}
	}()
	return ch, nil
}

func (cl *Client) singleStatSample(ctx context.Context, containerID string) (StatSample, error) {
	rc, err := cl.c.ContainerStats(ctx, containerID, false)
	if err != nil {
		return StatSample{}, err
	}
	defer rc.Body.Close()

	var s types.StatsJSON
	if err := json.NewDecoder(rc.Body).Decode(&s); err != nil {
		return StatSample{}, err
	}

	rssMB := int64(s.MemoryStats.Stats["rss"]) / (1024 * 1024)
	return StatSample{
		ContainerID: containerID,
		RSSMB:       rssMB,
		Timestamp:   time.Now(),
	}, nil
}

// RunStagingContainer creates and starts a container from imageTag under name,
// optionally exposing a single port. Returns the container ID.
// Existing containers with the same name are removed first (from failed previous runs).
func (cl *Client) RunStagingContainer(ctx context.Context, imageTag, name string, port int) (string, error) {
	// Clean up any existing container with this name (best-effort from failed previous runs).
	if err := cl.ContainerStop(ctx, name); err != nil {
		slog.Warn("docker: pre-cleanup stop failed", "container", name, "err", err)
	}
	if err := cl.ContainerRemove(ctx, name); err != nil {
		slog.Warn("docker: pre-cleanup remove failed", "container", name, "err", err)
	}

	cfg := &container.Config{
		Image: imageTag,
	}
	hostCfg := &container.HostConfig{
		AutoRemove: false, // we manage removal explicitly in cleanup
	}
	id, err := cl.ContainerCreate(ctx, cfg, hostCfg, name)
	if err != nil {
		return "", fmt.Errorf("docker: run staging container: %w", err)
	}
	if err := cl.ContainerStart(ctx, id); err != nil {
		_ = cl.ContainerRemove(ctx, id)
		return "", fmt.Errorf("docker: start staging container: %w", err)
	}
	return id, nil
}

// PruneImages removes dangling images (not referenced by any container).
func (cl *Client) PruneImages(ctx context.Context) error {
	_, err := cl.c.ImagesPrune(ctx, filters.Args{})
	if err != nil {
		return fmt.Errorf("docker: prune images: %w", err)
	}
	return nil
}

// ListImagesByPrefix returns all image tags that start with the given prefix.
func (cl *Client) ListImagesByPrefix(ctx context.Context, prefix string) ([]string, error) {
	images, err := cl.c.ImageList(ctx, image.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("docker: list images: %w", err)
	}
	var tags []string
	for _, img := range images {
		for _, tag := range img.RepoTags {
			if len(tag) >= len(prefix) && tag[:len(prefix)] == prefix {
				tags = append(tags, tag)
			}
		}
	}
	return tags, nil
}

// RunningContainerCount returns the number of currently running Docker containers.
func (cl *Client) RunningContainerCount(ctx context.Context) (int, error) {
	f := filters.NewArgs()
	f.Add("status", "running")
	containers, err := cl.c.ContainerList(ctx, container.ListOptions{Filters: f})
	if err != nil {
		return 0, fmt.Errorf("docker: list running containers: %w", err)
	}
	return len(containers), nil
}

// ContainerSummary is a lightweight view of a running container.
type ContainerSummary struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Image string `json:"image"`
	State string `json:"state"`
}

// ListContainers returns running containers belonging to a docker-compose project.
func (cl *Client) ListContainers(ctx context.Context, projectSlug string) ([]ContainerSummary, error) {
	f := filters.NewArgs()
	f.Add("status", "running")
	if projectSlug != "" {
		f.Add("label", "com.docker.compose.project="+projectSlug)
	}
	containers, err := cl.c.ContainerList(ctx, container.ListOptions{Filters: f})
	if err != nil {
		return nil, fmt.Errorf("docker: list containers for project %s: %w", projectSlug, err)
	}
	out := make([]ContainerSummary, 0, len(containers))
	for _, c := range containers {
		name := c.ID[:12]
		if len(c.Names) > 0 {
			// Docker names are prefixed with "/".
			n := c.Names[0]
			if len(n) > 0 && n[0] == '/' {
				n = n[1:]
			}
			name = n
		}
		out = append(out, ContainerSummary{
			ID:    c.ID,
			Name:  name,
			Image: c.Image,
			State: c.State,
		})
	}
	return out, nil
}

// ExecCreate creates an exec instance in a container and returns the exec ID.
func (cl *Client) ExecCreate(ctx context.Context, containerID string, cmd []string) (string, error) {
	resp, err := cl.c.ContainerExecCreate(ctx, containerID, types.ExecConfig{
		Cmd:          cmd,
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          true,
	})
	if err != nil {
		return "", fmt.Errorf("docker: exec create in %s: %w", containerID, err)
	}
	return resp.ID, nil
}

// ExecAttach attaches to an exec instance and returns a hijacked connection
// for bidirectional I/O. The caller must close the returned response.
func (cl *Client) ExecAttach(ctx context.Context, execID string) (types.HijackedResponse, error) {
	resp, err := cl.c.ContainerExecAttach(ctx, execID, types.ExecStartCheck{
		Tty: true,
	})
	if err != nil {
		return types.HijackedResponse{}, fmt.Errorf("docker: exec attach %s: %w", execID, err)
	}
	return resp, nil
}

// ExecResize resizes the TTY of an exec instance.
func (cl *Client) ExecResize(ctx context.Context, execID string, height, width uint) error {
	return cl.c.ContainerExecResize(ctx, execID, container.ResizeOptions{
		Height: height,
		Width:  width,
	})
}
