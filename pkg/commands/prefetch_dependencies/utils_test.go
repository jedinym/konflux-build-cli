package prefetch_dependencies

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/konflux-ci/konflux-build-cli/pkg/cliwrappers"
	. "github.com/onsi/gomega"
)

func TestParseInput(t *testing.T) {
	g := NewWithT(t)

	t.Run("should parse input JSON object", func(t *testing.T) {
		input := `{"foo": "bar"}`
		data := parseInput(input)
		g.Expect(data).To(Equal(map[string]any{"foo": "bar"}))
	})

	t.Run("should parse input JSON array", func(t *testing.T) {
		input := `[{"foo": "bar"}, {"foo": "baz"}]`
		data := parseInput(input)
		g.Expect(data).To(Equal([]any{map[string]any{"foo": "bar"}, map[string]any{"foo": "baz"}}))
	})

	t.Run("should parse input JSON object with packages array", func(t *testing.T) {
		input := `{"packages": [{"foo": "bar"}, {"foo": "baz"}]}`
		data := parseInput(input)
		g.Expect(data).To(Equal(map[string]any{"packages": []any{map[string]any{"foo": "bar"}, map[string]any{"foo": "baz"}}}))
	})

	t.Run("should convert plain string to JSON object", func(t *testing.T) {
		input := "foo"
		data := parseInput(input)
		g.Expect(data).To(Equal(map[string]any{"type": "foo"}))
	})
}

func TestContainsRPM(t *testing.T) {
	g := NewWithT(t)

	t.Run("should return false for empty object", func(t *testing.T) {
		input := `{}`
		data := parseInput(input)
		g.Expect(containsRPM(data)).To(BeFalse())
	})

	t.Run("should return false for empty array", func(t *testing.T) {
		input := `[]`
		data := parseInput(input)
		g.Expect(containsRPM(data)).To(BeFalse())
	})

	t.Run("should return true for RPM package", func(t *testing.T) {
		input := `{"type": "rpm"}`
		data := parseInput(input)
		g.Expect(containsRPM(data)).To(BeTrue())
	})

	t.Run("should return false for non-RPM package", func(t *testing.T) {
		input := `{"type": "yarn"}`
		data := parseInput(input)
		g.Expect(containsRPM(data)).To(BeFalse())
	})

	t.Run("should return true if any item in packages array is RPM", func(t *testing.T) {
		input := `{"packages": [{"type": "rpm"}, {"type": "yarn"}]}`
		data := parseInput(input)
		g.Expect(containsRPM(data)).To(BeTrue())
	})

	t.Run("should return true if any item in top-level array is RPM", func(t *testing.T) {
		input := `[{"type": "rpm"}, {"type": "yarn"}]`
		data := parseInput(input)
		g.Expect(containsRPM(data)).To(BeTrue())
	})
}

func TestInjectSummaryInSBOMField(t *testing.T) {
	g := NewWithT(t)

	t.Run("should inject summary in SBOM field for an RPM package", func(t *testing.T) {
		input := `{"type": "rpm"}`
		data := parseInput(input)
		g.Expect(injectSummaryInSBOMField(data)).To(Equal(map[string]any{"type": "rpm", "include_summary_in_sbom": true}))
	})
}

func TestInjectSSLOptions(t *testing.T) {
	g := NewWithT(t)

	exampleSSLOptions := map[string]any{
		"client_key":  "client_key",
		"client_cert": "client_cert",
		"ca_bundle":   "ca_bundle",
	}

	t.Run("should inject SSL options", func(t *testing.T) {
		input := `{"type": "rpm"}`
		data := parseInput(input)
		g.Expect(injectSSLOptions(data, exampleSSLOptions)).To(Equal(map[string]any{"type": "rpm", "options": map[string]any{"ssl": exampleSSLOptions}}))
	})

	t.Run("should overwrite existing SSL options", func(t *testing.T) {
		input := `{"type": "rpm", "options": {"ssl": {"client_key": "my_client_key"}}}`
		data := parseInput(input)
		g.Expect(injectSSLOptions(data, exampleSSLOptions)).To(Equal(map[string]any{"type": "rpm", "options": map[string]any{"ssl": map[string]any{"client_key": "client_key", "client_cert": "client_cert", "ca_bundle": "ca_bundle"}}}))
	})
}

func TestGetHostnameFromRemoteOriginURL(t *testing.T) {
	g := NewWithT(t)

	t.Run("should get hostname from HTTPS origin URL", func(t *testing.T) {
		tempDir := t.TempDir()
		executor := cliwrappers.NewCliExecutor()

		gitInit := cliwrappers.Command("git", "init")
		gitInit.Dir = tempDir
		gitRemoteAdd := cliwrappers.Command("git", "remote", "add", "origin", "https://github.com/user/repo.git")
		gitRemoteAdd.Dir = tempDir

		_, _, _, err := executor.Execute(gitInit)
		g.Expect(err).ToNot(HaveOccurred())
		_, _, _, err = executor.Execute(gitRemoteAdd)
		g.Expect(err).ToNot(HaveOccurred())

		hostname, err := getHostnameFromRemoteOriginURL(tempDir)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(hostname).To(Equal("github.com"))
	})
}

func TestDropGoProxyFromConfigFile(t *testing.T) {
	g := NewWithT(t)

	t.Run("should drop go proxy field from config file", func(t *testing.T) {
		const originalContent = `
		# comment
		gomod:
		  foo: bar
		  proxy_url: https://example.com
		`
		const expectedContent = `
		# comment
		gomod:
		  foo: bar
		`

		configFile := filepath.Join(t.TempDir(), "config.yaml")
		g.Expect(os.WriteFile(configFile, []byte(originalContent), 0644)).To(Succeed())

		g.Expect(dropGoProxyFrom(configFile)).To(Succeed())

		result, err := os.ReadFile(configFile)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(string(result)).To(Equal(expectedContent))
	})

	t.Run("should drop deprecated go proxy field from config file", func(t *testing.T) {
		const originalContent = `
		# comment
		foo: bar
		goproxy_url: https://example.com
		`
		const expectedContent = `
		# comment
		foo: bar
		`

		configFile := filepath.Join(t.TempDir(), "config.yaml")
		g.Expect(os.WriteFile(configFile, []byte(originalContent), 0644)).To(Succeed())

		g.Expect(dropGoProxyFrom(configFile)).To(Succeed())

		result, err := os.ReadFile(configFile)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(string(result)).To(Equal(expectedContent))
	})
}
