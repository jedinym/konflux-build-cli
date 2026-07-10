package integration_tests_framework

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/konflux-ci/konflux-build-cli/integration_tests/constants"
)

// https://github.com/podman-container-tools/container-libs/blob/b17b0a4c55bb953de8d783ea26534df22fdc9f2b/image/manifest/manifest.go#L56
var allContainerMediaTypes = []string{
	constants.OCIImageManifest,
	constants.DockerManifestV2,
	constants.DockerManifestV1JWS,
	constants.DockerManifestV1,
	constants.DockerManifestList,
	constants.OCIImageIndex,
}

type ImageRegistry interface {
	// Returns true for locally hosted registries.
	IsLocal() bool
	// Ensures all resources are ready to use the registry.
	Prepare() error
	// Starts local registry.
	Start() error
	// Stops local registry.
	Stop() error
	// Returns usename and password / token to access test namespace, see GetTestNamespace.
	GetCredentials() (string, string)
	// Returns base registry url, e.g. registry.io:1234
	GetRegistryDomain() string
	// Returns first part of the image name to which user can push test data.
	// Example: quay.io/my-org/
	GetTestNamespace() string
	// Returns config.josn content to authorize clients.
	GetDockerConfigJsonContent() []byte
	// Returns path to the root CA certificate the registry is using (in case of self-signed certificate),
	// empty string otherwise.
	GetCaCertPath() string
	// Do the http request, return the results of [http.Client.Do].
	// May also handle authentication automatically.
	DoRequest(req *http.Request) (*http.Response, error)
}

type ImageIndexManifest struct {
	MediaType   string          `json:"mediaType,omitempty"`
	Manifests   []ImageManifest `json:"manifests,omitempty"`
	RawManifest []byte          `json:"-"`
}
type ImageManifest struct {
	MediaType string `json:"mediaType,omitempty"`
	Digest    string `json:"digest,omitempty"`
}

func GenerateDockerAuthContent(registry, login, password string) ([]byte, error) {
	return GenerateDockerAuthContentWithAliases([]string{registry}, login, password)
}

func GenerateDockerAuthContentWithAliases(registries []string, login, password string) ([]byte, error) {
	type dockerConfigAuth struct {
		Auth string `json:"auth"`
	}
	type dockerConfigJson struct {
		Auths map[string]dockerConfigAuth `json:"auths"`
	}

	auth := dockerConfigAuth{Auth: base64.StdEncoding.EncodeToString([]byte(login + ":" + password))}
	auths := map[string]dockerConfigAuth{}
	for _, registry := range registries {
		auths[registry] = auth
	}
	dockerconfig := dockerConfigJson{Auths: auths}
	return json.Marshal(dockerconfig)
}

func stripRegistryDomain(imageName string) string {
	parts := strings.Split(imageName, "/")
	if len(parts) > 1 {
		parts = parts[1:]
	}
	return strings.Join(parts, "/")
}

// Check if the given manifest exists in the registry by sending
// a HEAD request to the registry's manifest endpoint.
func CheckManifestExistence(registry ImageRegistry, imageName, tagOrDigest string) (bool, error) {
	imageName = stripRegistryDomain(imageName)

	url := fmt.Sprintf("https://%s/v2/%s/manifests/%s", registry.GetRegistryDomain(), imageName, tagOrDigest)
	req, err := http.NewRequest("HEAD", url, nil)
	if err != nil {
		return false, err
	}

	req.Header["Accept"] = allContainerMediaTypes

	resp, err := registry.DoRequest(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	default:
		return false, fmt.Errorf("unexpected response code (expected 200 or 404): %d", resp.StatusCode)
	}
}

// Get the raw image manifest by sending a GET request
// to the registry's manifest endpoint.
func GetImageManifest(registry ImageRegistry, imageName, tag string) ([]byte, error) {
	imageName = stripRegistryDomain(imageName)

	url := fmt.Sprintf("https://%s/v2/%s/manifests/%s", registry.GetRegistryDomain(), imageName, tag)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header["Accept"] = allContainerMediaTypes

	resp, err := registry.DoRequest(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("received non-200 response status: %s", resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %v", err)
	}

	return body, nil
}

// Retrieve image index information by sending a GET request
// to the registry's manifest endpoint.
func GetImageIndexInfo(registry ImageRegistry, imageName, tag string) (*ImageIndexManifest, error) {
	body, err := GetImageManifest(registry, imageName, tag)
	if err != nil {
		return nil, fmt.Errorf("getting raw manifest: %w", err)
	}

	imageIndexInfo := &ImageIndexManifest{}
	if err := json.Unmarshal(body, imageIndexInfo); err != nil {
		return nil, fmt.Errorf("error unmarshaling response JSON: %v", err)
	}
	imageIndexInfo.RawManifest = body

	indexMediaTypes := []string{constants.OCIImageIndex, constants.DockerManifestList}
	if !slices.Contains(indexMediaTypes, imageIndexInfo.MediaType) {
		return nil, fmt.Errorf("expected an OCI index / docker manifest list, got %s", imageIndexInfo.MediaType)
	}

	return imageIndexInfo, nil
}
