package repository_test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/stretchr/testify/require"
)

// Review evidence for https://github.com/open-component-model/open-component-model/pull/4106
// at head 5ecc72904801f031d5147661bc76dca5249d9b1e. Review-only: this file is additive and
// changes no file the PR touches.
//
// PR #4106 adds, in push() (bindings/go/git/repository/upload.go):
//
//	if targetRef.IsTag() && !hasBranch(refs) { slog.WarnContext(ctx, "...no default branch...") }
//
// The guard keys off "is the pushed ref a tag" and "does any branch exist". It never asks
// whether the target's HEAD resolves - although the PR's own hint and GitUploaderConfig doc
// both name HEAD as the real condition ("push the branch the target's HEAD points at").
//
// F1 and F2 FAIL on the PR head and are the regression tests a fix must turn green.
// TestReviewPR4106_Probe passes and records the protocol fact that makes a fix cheap.
//
// Run from bindings/go:
//
//	go test ./git/repository/ -run 'TestReviewPR4106|TestUploadTagToEmptyRepositoryWarns' -race -v

// TestReviewPR4106_F1_BranchPushLeavesHeadUnresolved shows that a BRANCH push can leave the
// target in the same broken state the PR warns about, and stays silent.
//
// This is the fixture of the PR's own "branch into empty repo is silent" case
// (upload_warn_test.go): go-git's PlainInit points HEAD at refs/heads/master
// (go-git/v6 repository.go: defaultBranch: plumbing.Master) while the case pushes
// refs/heads/main. HEAD therefore dangles and a plain clone checks out no working tree.
//
// Note the empty-repository variant is NOT decidable before the push: an empty remote
// advertises nothing, so the uploader cannot learn which branch HEAD names. The actionable
// half of this finding is that the PR's test encodes "branch push => healthy", which its own
// fixture disproves.
//
// Expected (pass): the uploader warns, or the target's HEAD resolves.
// Actual on PR head (fail): no warning, HEAD dangles at refs/heads/master.
func TestReviewPR4106_F1_BranchPushLeavesHeadUnresolved(t *testing.T) {
	r := require.New(t)

	fixture := newRepository(t)
	uploader := newUploader(t)
	source, content := downloadResource(t, uploader, fixture.Path, "refs/heads/main", fixture.First)

	targetPath := filepath.Join(t.TempDir(), "target.git")
	_, err := git.PlainInit(targetPath, true)
	r.NoError(err)

	// Precondition: this bare target's HEAD names a branch the push below will not create.
	target, err := git.PlainOpen(targetPath)
	r.NoError(err)
	head, err := target.Reference(plumbing.HEAD, false)
	r.NoError(err)
	r.Equal(plumbing.SymbolicReference, head.Type())
	r.Equal("refs/heads/master", head.Target().String(), "go-git PlainInit points HEAD at master")

	capture := captureWarnings(t)
	_, err = uploader.UploadResource(t.Context(), targetResource(source, targetPath, "refs/heads/main"), content, nil)
	r.NoError(err)

	// The push created a branch, so the PR's hasBranch(refs) guard is satisfied.
	_, err = target.Reference("refs/heads/main", false)
	r.NoError(err)

	// But HEAD still dangles: resolving it fails exactly as in the tag-only case.
	_, resolveErr := target.Head()
	r.Error(resolveErr, "precondition: HEAD must be unresolvable for this finding to apply")

	// Independent confirmation through real git, when available.
	if gitBin, lookErr := exec.LookPath("git"); lookErr == nil {
		clonePath := filepath.Join(t.TempDir(), "clone")
		out, _ := exec.CommandContext(t.Context(), gitBin, "clone", targetPath, clonePath).CombinedOutput()
		r.Contains(string(out), "remote HEAD refers to nonexistent ref",
			"git clone must report the same broken HEAD as the tag-only case")
	}

	r.NotEmpty(capture.messages(),
		"a push that leaves the target's HEAD unresolvable must warn, not only a tag-into-branchless push; got no warning for refs/heads/main into a target whose HEAD is refs/heads/master. Joined messages: %q",
		strings.Join(capture.messages(), "\n"))
}

// TestReviewPR4106_F2_TagSuppressedWhileHeadDangles is the stronger form: the suppression
// branch of the new guard (!hasBranch(refs)) silences the warning on the TAG path itself
// while the target is still broken in exactly the way the PR describes.
//
// Fixture is the PR's own "tag into repo with a branch is silent" case: seed refs/heads/main
// into a go-git bare repo (HEAD -> refs/heads/master), then push refs/tags/rel-y. A branch
// now exists, so hasBranch(refs) is true and the warning is suppressed - but HEAD still names
// the absent refs/heads/master, so a plain clone checks out no working tree and Git hosts
// render the repository as empty.
//
// Expected (pass): the uploader warns, or the target's HEAD resolves.
// Actual on PR head (fail): warning suppressed while git clone still reports a broken HEAD.
func TestReviewPR4106_F2_TagSuppressedWhileHeadDangles(t *testing.T) {
	r := require.New(t)

	fixture := newRepository(t)
	uploader := newUploader(t)
	source, content := downloadResource(t, uploader, fixture.Path, "refs/heads/main", fixture.First)

	targetPath := filepath.Join(t.TempDir(), "target.git")
	_, err := git.PlainInit(targetPath, true)
	r.NoError(err)

	// Seed a branch whose name differs from the target's HEAD, satisfying hasBranch(refs).
	_, err = uploader.UploadResource(t.Context(), targetResource(source, targetPath, "refs/heads/main"), content, nil)
	r.NoError(err)

	capture := captureWarnings(t)
	_, err = uploader.UploadResource(t.Context(), targetResource(source, targetPath, "refs/tags/rel-y"), content, nil)
	r.NoError(err)

	target, err := git.PlainOpen(targetPath)
	r.NoError(err)

	// Precondition: a branch exists, so the PR suppresses the warning ...
	_, err = target.Reference("refs/heads/main", false)
	r.NoError(err)
	// ... yet HEAD still dangles, so the target is in the broken state the PR describes.
	_, resolveErr := target.Head()
	r.Error(resolveErr, "precondition: HEAD must be unresolvable for this finding to apply")

	gitBin, lookErr := exec.LookPath("git")
	r.NoError(lookErr, "this repro needs the git binary to confirm the clone symptom")
	clonePath := filepath.Join(t.TempDir(), "clone")
	out, _ := exec.CommandContext(t.Context(), gitBin, "clone", targetPath, clonePath).CombinedOutput()
	r.Contains(string(out), "remote HEAD refers to nonexistent ref",
		"precondition: the clone symptom must still be present")

	r.NotEmpty(capture.messages(),
		"a tag push into a target whose HEAD does not resolve must warn; hasBranch(refs) suppressed it because an unrelated branch exists. Joined messages: %q",
		strings.Join(capture.messages(), "\n"))
}

// TestReviewPR4106_Probe records the protocol fact that makes a precise fix cheap: the refs
// push() has already fetched via remote.ListContext carry HEAD as a SYMBOLIC reference when
// HEAD resolves, and omit HEAD entirely when it dangles. A server advertises HEAD only when it
// resolves, so `len(refs) > 0 && no HEAD in refs` means "HEAD dangles" with no extra round-trip.
//
// This test PASSES on the PR head; read its log output.
func TestReviewPR4106_Probe_AdvertisedHeadRevealsDanglingHead(t *testing.T) {
	for _, tc := range []struct {
		name     string
		headName string
		push     string
	}{
		{name: "head dangles (HEAD=master, pushed main)", headName: "refs/heads/master", push: "refs/heads/main"},
		{name: "head resolves (HEAD=main, pushed main)", headName: "refs/heads/main", push: "refs/heads/main"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			fixture := newRepository(t)
			uploader := newUploader(t)
			source, content := downloadResource(t, uploader, fixture.Path, "refs/heads/main", fixture.First)

			targetPath := filepath.Join(t.TempDir(), "target.git")
			tgt, err := git.PlainInit(targetPath, true)
			r.NoError(err)
			r.NoError(tgt.Storer.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.ReferenceName(tc.headName))))

			_, err = uploader.UploadResource(t.Context(), targetResource(source, targetPath, tc.push), content, nil)
			r.NoError(err)

			// List the target the same way push() does.
			probe, err := git.PlainInit(filepath.Join(t.TempDir(), "probe"), true)
			r.NoError(err)
			remote, err := probe.CreateRemote(&config.RemoteConfig{Name: "t", URLs: []string{targetPath}})
			r.NoError(err)
			refs, err := remote.ListContext(t.Context(), &git.ListOptions{PeelingOption: git.AppendPeeled})
			r.NoError(err)

			advertisesHead := false
			for _, ref := range refs {
				t.Logf("advertised: %-20s type=%v target=%q", ref.Name(), ref.Type(), ref.Target())
				if ref.Name() == plumbing.HEAD {
					advertisesHead = true
				}
			}
			_, resolveErr := func() (*plumbing.Reference, error) {
				opened, openErr := git.PlainOpen(targetPath)
				if openErr != nil {
					return nil, openErr
				}
				return opened.Head()
			}()
			r.Equal(resolveErr == nil, advertisesHead,
				"HEAD is advertised exactly when it resolves, so its absence is a reliable signal")
		})
	}
}

// TestReviewPR4106_FixDoesNotOverfire guards the suggested fix in
// .review/pr-4106/suggested-fix.patch against over-firing: a genuinely healthy target
// (HEAD -> refs/heads/main, main pushed, then a tag) must stay silent.
//
// It passes both on the PR head (where nothing warns) and with the patch applied, so it is a
// guard rather than a reproduction. Its value is only alongside the patch.
func TestReviewPR4106_FixDoesNotOverfire(t *testing.T) {
	r := require.New(t)
	fixture := newRepository(t)
	uploader := newUploader(t)
	source, content := downloadResource(t, uploader, fixture.Path, "refs/heads/main", fixture.First)

	targetPath := filepath.Join(t.TempDir(), "target.git")
	tgt, err := git.PlainInit(targetPath, true)
	r.NoError(err)
	r.NoError(tgt.Storer.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, "refs/heads/main")))

	_, err = uploader.UploadResource(t.Context(), targetResource(source, targetPath, "refs/heads/main"), content, nil)
	r.NoError(err)

	capture := captureWarnings(t)
	_, err = uploader.UploadResource(t.Context(), targetResource(source, targetPath, "refs/tags/rel-z"), content, nil)
	r.NoError(err)

	target, err := git.PlainOpen(targetPath)
	r.NoError(err)
	_, err = target.Head()
	r.NoError(err, "precondition: HEAD must resolve")

	r.Empty(capture.messages(), "a healthy target must not warn")
}
