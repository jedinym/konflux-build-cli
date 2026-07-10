package integration_tests

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"runtime"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	imagespecv1 "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/konflux-ci/konflux-build-cli/integration_tests/constants"
	. "github.com/konflux-ci/konflux-build-cli/integration_tests/framework"
	"github.com/konflux-ci/konflux-build-cli/pkg/commands"
	gitcmd "github.com/konflux-ci/konflux-build-cli/pkg/commands/gitclone"
	"github.com/konflux-ci/konflux-build-cli/pkg/common"
)

// TestBuildPipeline runs whole build pipeline end to end.
// It uses PVC approach implemented via a shared folder on host.
// Included tasks:
// - init
// - clone-repository
// - prefetch-dependencies
// - build-container
// - build-image-index
// - build-source-image
// - apply-tags
// - push-dockerfile
func TestBuildPipeline(t *testing.T) {
	var err error
	SetupGomega(t)

	imageRegistry := SetupImageRegistry(t)

	// Shared volume between containers
	workspaceDirHost, err := CreateTempDir("build-pipeline-")
	Expect(err).ToNot(HaveOccurred())
	// Use container to clean up the data since some files might be created with owner root.
	defer CleanupPath(workspaceDirHost)

	const sourceDir = "source"
	sourceDirHost := path.Join(workspaceDirHost, sourceDir)

	Expect(os.MkdirAll(sourceDirHost, 0755)).To(Succeed())
	// Chmod to 0777 to allow the container user to write to the directory.
	// Use a separate Chmod rather than passing 0777 to MkdirAll,
	// because MkdirAll respects umask so the result may not actually be 0777.
	Expect(os.Chmod(sourceDirHost, 0777)).To(Succeed())

	const gitUrl = "https://github.com/konflux-ci/konflux-build-cli"
	outputImageName := imageRegistry.GetTestNamespace() + "test-image-build"
	outputImageTag := "result"
	newTag := time.Now().Format("2006-01-02_15-04-05")
	newTagFromLabel := "label-" + newTag

	initResults := struct {
		httpProxy string
		noProxy   string
	}{}
	t.Run("init", func(t *testing.T) {
		SetupGomega(t)

		const konfluxConfigFilePath = "/etc/konflux-config"
		const httpProxyResultPath = "/tmp/http-proxy-result"
		const noProxyResultPath = "/tmp/no-proxy-result"

		container := NewBuildCliRunnerContainer("init", ApplyTagsImage)
		container.AddEnv("PLATFORM_CONFIG_FILE", konfluxConfigFilePath)

		err = container.Start()
		Expect(err).ToNot(HaveOccurred())
		t.Cleanup(func() { container.DeleteIfExists() })

		const konfluxConfigFile = `
		            [cache-proxy]
					allow-cache-proxy = false
					http-proxy = some.proxy.net
					no-proxy = localhost:1234
				`
		err = container.CreateFileInContainer(konfluxConfigFilePath, konfluxConfigFile)
		Expect(err).ToNot(HaveOccurred())

		cacheProxyArgs := []string{"config", "cache-proxy", "--enable", "false"}
		cacheProxyArgs = append(cacheProxyArgs, "--http-proxy-result-path", httpProxyResultPath)
		cacheProxyArgs = append(cacheProxyArgs, "--no-proxy-result-path", noProxyResultPath)

		err = container.ExecuteBuildCli(cacheProxyArgs...)
		Expect(err).ToNot(HaveOccurred())

		initResults.httpProxy, err = container.GetFileContent(httpProxyResultPath)
		Expect(err).ToNot(HaveOccurred())
		Expect(initResults.httpProxy).To(BeEmpty())
		initResults.noProxy, err = container.GetFileContent(noProxyResultPath)
		Expect(err).ToNot(HaveOccurred())
		Expect(initResults.noProxy).To(BeEmpty())
	})

	cloneResults := gitcmd.Results{}
	t.Run("clone-repository", func(t *testing.T) {
		SetupGomega(t)

		container := startGitCloneContainer(t, workspaceDirHost)

		args := []string{"git-clone", "--url", gitUrl, "--output-dir", path.Join("/workspace", sourceDir)}
		stdout, _, err := container.ExecuteBuildCliWithOutput(args...)
		Expect(err).ToNot(HaveOccurred(), "git clone failed")

		cloneResults, err = parseGitCloneResult(stdout)
		Expect(err).ToNot(HaveOccurred())
		Expect(cloneResults.Commit).ToNot(BeEmpty())
	})

	const prefetchDir = "hermeto"
	const prefetchOutputMountPoint = "/hermeto/output"
	prefetchOutputDir := path.Join(prefetchDir, "output")
	prefetchEnvJsonFile := path.Join(prefetchDir, "prefetch-env.json")
	t.Run("prefetch-dependencies", func(t *testing.T) {
		SetupGomega(t)

		err := runPrefetchDependencies(prefetchDependenciesTestParams{
			Input:               `{"packages": [{"type": "gomod"}]}`,
			Context:             sourceDirHost,
			OutputDir:           prefetchOutputDir,
			OutputDirMountPoint: prefetchOutputMountPoint,
			EnvFiles:            []string{prefetchEnvJsonFile},
		})
		Expect(err).ToNot(HaveOccurred(), "prefetch failed")
	})

	buildResults := &commands.BuildResults{}
	t.Run("build-container", func(t *testing.T) {
		SetupGomega(t)

		outputImageRef := outputImageName + ":" + runtime.GOARCH
		buildParams := BuildParams{
			Context:               sourceDirHost,
			OutputRef:             outputImageRef,
			Hermetic:              true,
			PrefetchDir:           path.Join("/workspace", prefetchDir),
			PrefetchOutputMount:   prefetchOutputMountPoint,
			Push:                  true,
			QuayImageExpiresAfter: "1h",
			Labels:                []string{fmt.Sprintf("%s=%s", KonfluxAdditionalTagsLabelName, newTagFromLabel)},
		}

		buildResults, err = RunBuild(buildParams, imageRegistry)

		Expect(err).ToNot(HaveOccurred(), "build failed")
		Expect(buildResults.ImageUrl).To(Equal(outputImageRef))
		Expect(buildResults.Digest).To(MatchRegexp("^sha256:[a-f0-9]{64}$"))

		exists, err := CheckManifestExistence(imageRegistry, common.GetImageName(buildResults.ImageUrl), buildResults.Digest)
		Expect(err).ToNot(HaveOccurred())
		Expect(exists).To(BeTrue())
	})

	buildImageIndexResults := &BuildImageIndexResults{}
	t.Run("build-image-index", func(t *testing.T) {
		SetupGomega(t)

		const additionalIndexTag = "latest"
		resultImageName := common.GetImageName(buildResults.ImageUrl)
		params := BuildImageIndexParams{
			Image: resultImageName + ":" + outputImageTag,
			Images: []string{
				resultImageName + "@" + buildResults.Digest,
			},
			BuildahFormat:    "oci",
			AdditionalTags:   []string{additionalIndexTag},
			AlwaysBuildIndex: new(true),
		}
		indexOutput, _, err := RunBuildImageIndex(params, imageRegistry, true)
		Expect(err).ToNot(HaveOccurred(), "build image index failed")
		buildImageIndexResults = indexOutput.Results
		Expect(buildImageIndexResults.ImageURL).To(Equal(outputImageName + ":" + outputImageTag))
		Expect(buildImageIndexResults.ImageRef).To(Equal(outputImageName + "@" + buildImageIndexResults.ImageDigest))
		Expect(buildImageIndexResults.ImageDigest).To(MatchRegexp("^sha256:[a-f0-9]{64}$"))
		Expect(buildImageIndexResults.Images).To(Equal(resultImageName + "@" + buildResults.Digest))

		imageIndexTags := []string{outputImageTag, additionalIndexTag}
		for _, tag := range imageIndexTags {
			exists, err := CheckManifestExistence(imageRegistry, outputImageName, tag)
			Expect(err).ToNot(HaveOccurred())
			Expect(exists).To(BeTrue())
		}

		imageIndexInfo, err := GetImageIndexInfo(imageRegistry, outputImageName, outputImageTag)
		Expect(err).ToNot(HaveOccurred(), fmt.Sprintf("failed to get image index %s:%s", outputImageName, outputImageTag))
		Expect(imageIndexInfo.MediaType).To(Equal(constants.OCIImageIndex))
		Expect(imageIndexInfo.Manifests).To(HaveLen(1))

		manifestInfo := imageIndexInfo.Manifests[0]
		Expect(manifestInfo.MediaType).To(BeElementOf([]string{constants.OCIImageManifest, constants.DockerManifestV2}))
		Expect(manifestInfo.Digest).To(Equal(buildResults.Digest))
	})

	t.Run("build-source-image", func(t *testing.T) {
		SetupGomega(t)
		// TODO implement when build-source-image is ported to the CLI
	})

	t.Run("apply-tags", func(t *testing.T) {
		SetupGomega(t)

		applyTagsParams := ApplyTagsParams{
			ImageRepoUrl: outputImageName,
			ImageDigest:  buildImageIndexResults.ImageDigest,
			Tags:         []string{newTag},
		}

		err := RunApplyTags(applyTagsParams, imageRegistry)
		Expect(err).ToNot(HaveOccurred(), "apply tags failed")

		for _, tag := range []string{newTag, newTagFromLabel} {
			tagExists, err := CheckManifestExistence(imageRegistry, outputImageName, tag)
			Expect(err).ToNot(HaveOccurred(), fmt.Sprintf("failed to check for %s tag existence", tag))
			Expect(tagExists).To(BeTrue(), fmt.Sprintf("expected %s:%s to exist", outputImageName, tag))

			imageIndexInfo, err := GetImageIndexInfo(imageRegistry, outputImageName, tag)
			Expect(err).ToNot(HaveOccurred(), fmt.Sprintf("failed to get image index %s:%s", outputImageName, tag))
			Expect(imageIndexInfo.MediaType).To(BeElementOf([]string{constants.OCIImageIndex, constants.DockerManifestList}))
		}
	})

	pushContainerfileResults := &commands.PushContainerfileResults{}
	t.Run("push-dockerfile", func(t *testing.T) {
		SetupGomega(t)

		const tagSuffix = ".containerfile"
		pushContainerfileParams := PushContainerfileParams{
			imageUrl:  outputImageName,
			digest:    buildImageIndexResults.ImageDigest,
			source:    "/workspace/source",
			tagSuffix: tagSuffix,
		}
		pushContainerfileResults, err = RunPushContainerfile(pushContainerfileParams, imageRegistry, sourceDirHost)
		Expect(err).ToNot(HaveOccurred(), "push containerfile failed")

		containerfileDigest := common.GetImageDigest(pushContainerfileResults.ImageRef)
		Expect(containerfileDigest).To(MatchRegexp("^sha256:[a-f0-9]{64}$"))
		manifestExists, err := CheckManifestExistence(imageRegistry, outputImageName, containerfileDigest)
		Expect(err).ToNot(HaveOccurred())
		Expect(manifestExists).To(BeTrue())

		expectedContainerfileTag := strings.ReplaceAll(buildImageIndexResults.ImageDigest, ":", "-") + tagSuffix
		tagExists, err := CheckManifestExistence(imageRegistry, outputImageName, expectedContainerfileTag)
		Expect(err).ToNot(HaveOccurred(), fmt.Sprintf("failed to check for %s tag existence", expectedContainerfileTag))
		Expect(tagExists).To(BeTrue(), fmt.Sprintf("expected %s:%s to exist", outputImageName, expectedContainerfileTag))

		// Verify content of the pushed Containerfile
		manifestJsonBytes, err := GetImageManifest(imageRegistry, outputImageName, containerfileDigest)
		Expect(err).ToNot(HaveOccurred(), fmt.Sprintf("Expected %s:%s to exist in registry", outputImageName, containerfileDigest))

		var manifest imagespecv1.Manifest
		err = json.Unmarshal(manifestJsonBytes, &manifest)
		Expect(err).ToNot(HaveOccurred())
		Expect(manifest.MediaType).To(Equal(constants.OCIImageManifest))

		Expect(manifest.Layers).To(HaveLen(1))
		layerDescriptor := manifest.Layers[0]
		Expect(layerDescriptor.Annotations).To(HaveKeyWithValue("org.opencontainers.image.title", "Dockerfile"))

		containerfileContent, err := os.ReadFile(path.Join(sourceDirHost, "Dockerfile"))
		Expect(err).ToNot(HaveOccurred())
		expectedLayerDigest := "sha256:" + sha256Checksum(string(containerfileContent))
		Expect(string(layerDescriptor.Digest)).To(Equal(expectedLayerDigest))
	})
}
