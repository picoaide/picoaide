package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
)

const (
	NetworkName   = "picoaide-net"
	NetworkSubnet = "100.64.0.0/16"
	NetworkGW     = "100.64.0.1"
)

var cli *client.Client

// InitClient 初始化 Docker Engine API 客户端
func InitClient() error {
	var err error
	cli, err = client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return fmt.Errorf("连接 Docker daemon 失败: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = cli.Ping(ctx)
	if err != nil {
		return fmt.Errorf("Docker daemon 不可达: %w", err)
	}
	return nil
}

// CloseClient 关闭 Docker 客户端
func CloseClient() {
	if cli != nil {
		cli.Close()
	}
}

// EnsureNetwork 创建 picoaide-net 网络（如不存在）
func EnsureNetwork(ctx context.Context) error {
	args := filters.NewArgs()
	args.Add("name", NetworkName)
	nets, err := cli.NetworkList(ctx, network.ListOptions{Filters: args})
	if err != nil {
		return fmt.Errorf("查询网络失败: %w", err)
	}
	for _, n := range nets {
		if n.Name == NetworkName {
			return nil
		}
	}

	_, err = cli.NetworkCreate(ctx, NetworkName, network.CreateOptions{
		Driver: "bridge",
		IPAM: &network.IPAM{
			Driver: "default",
			Config: []network.IPAMConfig{
				{Subnet: NetworkSubnet, Gateway: NetworkGW},
			},
		},
		Options: map[string]string{
			"com.docker.network.bridge.enable_icc": "false",
		},
		Internal: false,
	})
	if err != nil {
		return fmt.Errorf("创建网络 %s 失败: %w", NetworkName, err)
	}
	fmt.Printf("已创建网络 %s (%s, ICC=false)\n", NetworkName, NetworkSubnet)
	return nil
}

// CreateContainer 创建用户容器（bind mount + 静态 IP + 资源限制）
func CreateContainer(ctx context.Context, username, imageRef, userDir, ip string, cpuLimit float64, memMB int64) (string, error) {
	containerName := "picoaide-" + username

	// 如果同名容器已存在，先移除
	existing, _ := cli.ContainerInspect(ctx, containerName)
	if existing.ContainerJSONBase != nil {
		_ = cli.ContainerRemove(ctx, containerName, container.RemoveOptions{Force: true})
	}

	var mounts []mount.Mount
	mounts = append(mounts, mount.Mount{
		Type:   mount.TypeBind,
		Source: userDir + "/root",
		Target: "/root",
	})

	hostConfig := &container.HostConfig{
		Mounts:       mounts,
		RestartPolicy: container.RestartPolicy{Name: "unless-stopped"},
	}
	if cpuLimit > 0 {
		hostConfig.Resources.NanoCPUs = int64(cpuLimit * 1e9)
	}
	if memMB > 0 {
		hostConfig.Resources.Memory = memMB * 1024 * 1024
	}

	netConfig := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			NetworkName: {
				IPAMConfig: &network.EndpointIPAMConfig{
					IPv4Address: ip,
				},
			},
		},
	}

	config := &container.Config{
		Image: imageRef,
		Env:   []string{"TZ=Asia/Shanghai"},
	}

	resp, err := cli.ContainerCreate(ctx, config, hostConfig, netConfig, nil, containerName)
	if err != nil {
		return "", fmt.Errorf("创建容器失败: %w", err)
	}
	return resp.ID, nil
}

// Start 启动容器
func Start(ctx context.Context, containerID string) error {
	return cli.ContainerStart(ctx, containerID, container.StartOptions{})
}

// Stop 停止容器
func Stop(ctx context.Context, containerID string) error {
	timeout := 10
	return cli.ContainerStop(ctx, containerID, container.StopOptions{Timeout: &timeout})
}

// Restart 重启容器
func Restart(ctx context.Context, containerID string) error {
	timeout := 10
	return cli.ContainerRestart(ctx, containerID, container.StopOptions{Timeout: &timeout})
}

// Remove 移除容器
func Remove(ctx context.Context, containerID string) error {
	return cli.ContainerRemove(ctx, containerID, container.RemoveOptions{Force: true})
}

// ContainerStatus 返回容器状态字符串（running / exited / ...）
func ContainerStatus(ctx context.Context, containerID string) string {
	if containerID == "" {
		return "未创建"
	}
	inspect, err := cli.ContainerInspect(ctx, containerID)
	if err != nil {
		return "未知"
	}
	return inspect.State.Status
}

// ContainerRunning 返回容器是否运行中
func ContainerRunning(ctx context.Context, containerID string) bool {
	if containerID == "" {
		return false
	}
	inspect, err := cli.ContainerInspect(ctx, containerID)
	if err != nil {
		return false
	}
	return inspect.State.Running
}

// ContainerExists 按容器名检查容器是否存在
func ContainerExists(ctx context.Context, username string) bool {
	_, err := cli.ContainerInspect(ctx, "picoaide-"+username)
	return err == nil
}

// GetContainerIDByName 按容器名获取容器 ID
func GetContainerIDByName(ctx context.Context, username string) (string, error) {
	inspect, err := cli.ContainerInspect(ctx, "picoaide-"+username)
	if err != nil {
		return "", err
	}
	return inspect.ID, nil
}

// ImageExists 检查镜像是否已存在于本地
func ImageExists(ctx context.Context, imageRef string) bool {
	_, _, err := cli.ImageInspectWithRaw(ctx, imageRef)
	return err == nil
}

// ImagePull 拉取镜像，返回响应体供 SSE 流式读取
func ImagePull(ctx context.Context, imageRef string) (io.ReadCloser, error) {
	return cli.ImagePull(ctx, imageRef, image.PullOptions{})
}

// ListLocalImages 列出本地镜像（按仓库前缀过滤）
func ListLocalImages(ctx context.Context, repoPrefix string) ([]image.Summary, error) {
	imgs, err := cli.ImageList(ctx, image.ListOptions{})
	if err != nil {
		return nil, err
	}
	if repoPrefix == "" {
		return imgs, nil
	}
	var filtered []image.Summary
	for _, img := range imgs {
		for _, tag := range img.RepoTags {
			if strings.HasPrefix(tag, repoPrefix) {
				filtered = append(filtered, img)
				break
			}
		}
	}
	return filtered, nil
}

// RegistryTag ghcr.io 远程标签
type RegistryTag struct {
	Name string `json:"name"`
}

// ListRegistryTags 从 ghcr.io 获取镜像所有标签
func ListRegistryTags(ctx context.Context, repo string) ([]string, error) {
	// 先获取匿名 token
	tokenURL := fmt.Sprintf("https://ghcr.io/token?service=ghcr.io&scope=repository:%s:pull", repo)
	token := ""
	{
		tReq, _ := http.NewRequestWithContext(ctx, "GET", tokenURL, nil)
		tResp, err := http.DefaultClient.Do(tReq)
		if err == nil {
			defer tResp.Body.Close()
			var tokenData struct {
				Token string `json:"token"`
			}
			if json.NewDecoder(tResp.Body).Decode(&tokenData) == nil {
				token = tokenData.Token
			}
		}
	}

	url := fmt.Sprintf("https://ghcr.io/v2/%s/tags/list", repo)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求 ghcr.io 失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ghcr.io 返回 %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Tags []string `json:"tags"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("解析标签列表失败: %w", err)
	}
	return result.Tags, nil
}

// parseWWWAuthenticate 从 WWW-Authenticate 头提取 token URL
func parseWWWAuthenticate(header string) string {
	// Bearer realm="https://ghcr.io/token",service="ghcr.io",scope="repository:repo:pull"
	parts := strings.Split(header, ",")
	var realm, service, scope string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if strings.HasPrefix(p, `realm="`) {
			realm = strings.TrimSuffix(strings.TrimPrefix(p, `realm="`), `"`)
		} else if strings.HasPrefix(p, `service="`) {
			service = strings.TrimSuffix(strings.TrimPrefix(p, `service="`), `"`)
		} else if strings.HasPrefix(p, `scope="`) {
			scope = strings.TrimSuffix(strings.TrimPrefix(p, `scope="`), `"`)
		}
	}
	if realm == "" {
		return ""
	}
	return fmt.Sprintf("%s?service=%s&scope=%s", realm, service, scope)
}
