package container_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/osbuild/images/pkg/arch"
	"github.com/osbuild/images/pkg/rpmmd"

	"github.com/osbuild/images/pkg/bib/container"
	"github.com/osbuild/images/pkg/bib/osinfo"
)

const (
	dnfTestingImageRHEL         = "registry.access.redhat.com/ubi9:latest"
	dnfTestingImageCentos       = "quay.io/centos/centos:stream9"
	dnfTestingImageFedoraLatest = "registry.fedoraproject.org/fedora:latest"
)

func ensureCanRunDNFJsonTests(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("skipping test; not running as root")
	}
	if _, err := os.Stat("/usr/libexec/osbuild-depsolve-dnf"); err != nil {
		t.Skip("cannot find /usr/libexec/osbuild-depsolve-dnf")
	}
}

func ensureAMD64(t *testing.T) {
	if runtime.GOARCH != "amd64" {
		t.Skip("skipping test; only runs on x86_64")
	}
}

func TestDNFJsonWorks(t *testing.T) {
	if !hasPodman() {
		t.Skip("skipping test: no podman")
	}

	ensureCanRunDNFJsonTests(t)

	cacheRoot := t.TempDir()

	cnt, err := container.New(dnfTestingImageCentos)
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, cnt.Stop())
	}()

	err = cnt.InitDNF()
	require.NoError(t, err)

	sourceInfo, err := osinfo.Load(cnt.Root())
	require.NoError(t, err)
	solver, err := cnt.NewContainerSolver(cacheRoot, arch.Current(), sourceInfo)
	require.NoError(t, err)
	res, err := solver.Depsolve([]rpmmd.PackageSet{
		{
			Include: []string{"coreutils"},
		},
	}, 0)
	require.NoError(t, err)
	assert.True(t, len(res.Packages) > 0)
}

func subscribeMachine(t *testing.T) (restore func()) {
	if _, err := exec.LookPath("subscription-manager"); err != nil {
		t.Skip("no subscription-manager found")
		return func() {}
	}

	matches, err := filepath.Glob("/etc/pki/entitlement/*.pem")
	if err == nil && len(matches) > 0 {
		return func() {}
	}

	rhsmOrg := os.Getenv("RHSM_ORG")
	rhsmActivationKey := os.Getenv("RHSM_ACTIVATION_KEY")
	if rhsmOrg == "" || rhsmActivationKey == "" {
		t.Skip("no RHSM_{ORG,ACTIVATION_KEY} env vars found")
		return func() {}
	}

	err = exec.Command("subscription-manager", "register",
		"--org", rhsmOrg,
		"--activationkey", rhsmActivationKey).Run()
	require.NoError(t, err)

	return func() {
		err := exec.Command("subscription-manager", "unregister").Run()
		require.NoError(t, err)
	}
}

func TestDNFInitGivesAccessToSubscribedContent(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("skipping test; not running as root")
	}
	ensureAMD64(t)

	restore := subscribeMachine(t)
	defer restore()

	cnt, err := container.New(dnfTestingImageRHEL)
	require.NoError(t, err)
	err = cnt.InitDNF()
	require.NoError(t, err)

	content, err := cnt.ReadFile("/etc/yum.repos.d/redhat.repo")
	require.NoError(t, err)
	assert.Contains(t, string(content), "rhel-9-for-x86_64-baseos-rpms")
}

func TestDNFJsonWorkWithSubscribedContent(t *testing.T) {
	ensureCanRunDNFJsonTests(t)
	ensureAMD64(t)
	cacheRoot := t.TempDir()

	restore := subscribeMachine(t)
	defer restore()

	cnt, err := container.New(dnfTestingImageRHEL)
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, cnt.Stop())
	}()

	err = cnt.InitDNF()
	require.NoError(t, err)

	sourceInfo, err := osinfo.Load(cnt.Root())
	require.NoError(t, err)
	solver, err := cnt.NewContainerSolver(cacheRoot, arch.ARCH_X86_64, sourceInfo)
	require.NoError(t, err)

	res, err := solver.Depsolve([]rpmmd.PackageSet{
		{
			Include: []string{"coreutils"},
		},
	}, 0)
	require.NoError(t, err)
	assert.True(t, len(res.Packages) > 0)
}

func runCmd(t *testing.T, args ...string) {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	require.NoError(t, err)
}

func TestDNFJsonWorkWithSubscribedContentNestedContainers(t *testing.T) {
	ensureCanRunDNFJsonTests(t)
	ensureAMD64(t)
	tmpdir := t.TempDir()

	restore := subscribeMachine(t)
	defer restore()

	// build a test binary from the existing
	// TestDNFJsonWorkWithSubscribedContent that is then
	// transfered and run *inside* the centos container
	testBinary := filepath.Join(tmpdir, "dnftest")
	runCmd(t, "go", "test",
		"-c",
		"-o", testBinary,
		"-run", "^TestDNFJsonWorkWithSubscribedContent$")

	output, err := exec.Command(
		"podman", "run", "--rm",
		"--privileged",
		"--init",
		"--detach",
		"--entrypoint", "sleep",
		// use a fedora container as intermediate so that we
		// always have the latest glibc (we cannot fully
		// static link the test)
		dnfTestingImageFedoraLatest,
		"infinity",
	).Output()
	require.NoError(t, err, string(output))
	cntID := strings.TrimSpace(string(output))
	defer func() {
		err := exec.Command("podman", "stop", cntID).Run()
		assert.NoError(t, err)
	}()

	runCmd(t, "podman", "cp", testBinary, cntID+":/dnftest")
	// we need these test dependencies inside the container
	runCmd(t, "podman", "exec", cntID, "dnf", "install", "-y",
		"gpgme", "podman")
	// run the test
	runCmd(t, "podman", "exec", cntID, "/dnftest")
}
