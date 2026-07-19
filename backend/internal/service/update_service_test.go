//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type updateServiceCacheStub struct {
	data string
}

func (s *updateServiceCacheStub) GetUpdateInfo(context.Context) (string, error) {
	if s.data == "" {
		return "", errors.New("cache miss")
	}
	return s.data, nil
}

func (s *updateServiceCacheStub) SetUpdateInfo(_ context.Context, data string, _ time.Duration) error {
	s.data = data
	return nil
}

type updateServiceGitHubClientStub struct {
	release        *GitHubRelease
	recentReleases []*GitHubRelease
	recentErr      error
}

func (s *updateServiceGitHubClientStub) FetchLatestRelease(context.Context, string) (*GitHubRelease, error) {
	return s.release, nil
}

func (s *updateServiceGitHubClientStub) FetchRecentReleases(context.Context, string, int) ([]*GitHubRelease, error) {
	return s.recentReleases, s.recentErr
}

func (s *updateServiceGitHubClientStub) DownloadFile(context.Context, string, string, int64) error {
	panic("DownloadFile should not be called when no update is available")
}

func (s *updateServiceGitHubClientStub) FetchChecksumFile(context.Context, string) ([]byte, error) {
	panic("FetchChecksumFile should not be called when no update is available")
}

func TestUpdateServicePerformUpdateNoUpdateReturnsSentinel(t *testing.T) {
	svc := NewUpdateService(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{
			release: &GitHubRelease{
				TagName: "v0.1.132",
				Name:    "v0.1.132",
			},
		},
		"0.1.132",
		"release",
	)

	err := svc.PerformUpdate(context.Background())

	require.Error(t, err)
	require.True(t, errors.Is(err, ErrNoUpdateAvailable))
	require.ErrorIs(t, err, ErrNoUpdateAvailable)
}

func newRollbackTestService(current string, releases []*GitHubRelease) *UpdateService {
	return NewUpdateService(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{recentReleases: releases},
		current,
		"release",
	)
}

func TestUpdateServiceListRollbackVersionsFiltersAndCaps(t *testing.T) {
	releases := []*GitHubRelease{
		{TagName: "v0.1.148", PublishedAt: "2026-07-09T00:00:00Z"},                       // newer than current: excluded
		{TagName: "v0.1.147", PublishedAt: "2026-07-08T00:00:00Z"},                       // current: excluded
		{TagName: "v0.1.146-rc1", PublishedAt: "2026-07-07T12:00:00Z", Prerelease: true}, // prerelease: excluded
		{TagName: "v0.1.146", PublishedAt: "2026-07-07T00:00:00Z"},
		{TagName: "v0.1.145", PublishedAt: "2026-07-06T00:00:00Z", Draft: true}, // draft: excluded
		{TagName: "v0.1.144", PublishedAt: "2026-07-05T00:00:00Z"},
		{TagName: "v0.1.144", PublishedAt: "2026-07-05T00:00:00Z"}, // duplicate: excluded
		{TagName: "v0.1.143", PublishedAt: "2026-07-04T00:00:00Z"},
		{TagName: "v0.1.142", PublishedAt: "2026-07-03T00:00:00Z"}, // beyond cap of 3: excluded
	}
	svc := newRollbackTestService("0.1.147", releases)

	versions, err := svc.ListRollbackVersions(context.Background())

	require.NoError(t, err)
	require.Len(t, versions, 3)
	require.Equal(t, "0.1.146", versions[0].Version)
	require.Equal(t, "0.1.144", versions[1].Version)
	require.Equal(t, "0.1.143", versions[2].Version)
}

func TestUpdateServiceListRollbackVersionsSortsUnorderedInput(t *testing.T) {
	releases := []*GitHubRelease{
		{TagName: "v0.1.144"},
		{TagName: "v0.1.146"},
		{TagName: "v0.1.145"},
	}
	svc := newRollbackTestService("0.1.147", releases)

	versions, err := svc.ListRollbackVersions(context.Background())

	require.NoError(t, err)
	require.Len(t, versions, 3)
	require.Equal(t, "0.1.146", versions[0].Version)
	require.Equal(t, "0.1.145", versions[1].Version)
	require.Equal(t, "0.1.144", versions[2].Version)
}

func TestUpdateServiceListRollbackVersionsMixedCustomAndUpstream(t *testing.T) {
	releases := []*GitHubRelease{
		{TagName: "v0.1.157", PublishedAt: "2026-07-20T00:00:00Z"},                        // 比当前新:排除
		{TagName: "v0.1.156-custom.1", PublishedAt: "2026-07-16T00:00:00Z"},               // 当前版本:排除
		{TagName: "v0.1.156", PublishedAt: "2026-07-15T00:00:00Z"},                        // 比当前旧:入选
		{TagName: "v0.1.156-rc.1", PublishedAt: "2026-07-14T12:00:00Z", Prerelease: true}, // prerelease:排除
		{TagName: "v0.1.155-custom.2", PublishedAt: "2026-07-14T00:00:00Z"},               // 比当前旧:入选
		{TagName: "v0.1.155", PublishedAt: "2026-07-13T00:00:00Z"},                        // 比当前旧:入选
	}
	svc := newRollbackTestService("0.1.156-custom.1", releases)

	versions, err := svc.ListRollbackVersions(context.Background())

	require.NoError(t, err)
	require.Len(t, versions, 3)
	require.Equal(t, "0.1.156", versions[0].Version)
	require.Equal(t, "0.1.155-custom.2", versions[1].Version)
	require.Equal(t, "0.1.155", versions[2].Version)
}

func TestUpdateServiceListRollbackVersionsEmptyWhenNoneOlder(t *testing.T) {
	releases := []*GitHubRelease{
		{TagName: "v0.1.147"},
		{TagName: "v0.1.148"},
	}
	svc := newRollbackTestService("0.1.147", releases)

	versions, err := svc.ListRollbackVersions(context.Background())

	require.NoError(t, err)
	require.Empty(t, versions)
}

func TestUpdateServiceListRollbackVersionsPropagatesFetchError(t *testing.T) {
	svc := NewUpdateService(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{recentErr: errors.New("github unavailable")},
		"0.1.147",
		"release",
	)

	_, err := svc.ListRollbackVersions(context.Background())

	require.Error(t, err)
	require.Contains(t, err.Error(), "github unavailable")
}

func TestUpdateServiceRollbackToVersionRejectsDisallowedTargets(t *testing.T) {
	releases := []*GitHubRelease{
		{TagName: "v0.1.148"},
		{TagName: "v0.1.147"},
		{TagName: "v0.1.146"},
		{TagName: "v0.1.145"},
		{TagName: "v0.1.144"},
		{TagName: "v0.1.143"},
		{TagName: "v0.1.142"},
	}
	svc := newRollbackTestService("0.1.147", releases)

	for _, target := range []string{
		"",         // empty
		"0.1.147",  // current version
		"v0.1.147", // current version with prefix
		"0.1.148",  // newer than current
		"0.1.142",  // older than the 3 most recent
		"9.9.9",    // nonexistent
	} {
		err := svc.RollbackToVersion(context.Background(), target)
		require.ErrorIs(t, err, ErrRollbackVersionNotAllowed, "target %q should be rejected", target)
	}
}

func TestUpdateServiceRollbackToVersionAcceptsVPrefix(t *testing.T) {
	// No platform asset in the release: the target passes the allowlist check
	// and fails later at asset lookup, proving the version itself was accepted.
	releases := []*GitHubRelease{
		{TagName: "v0.1.147"},
		{TagName: "v0.1.146"},
	}
	svc := newRollbackTestService("0.1.147", releases)

	err := svc.RollbackToVersion(context.Background(), "v0.1.146")

	require.Error(t, err)
	require.NotErrorIs(t, err, ErrRollbackVersionNotAllowed)
	require.Contains(t, err.Error(), "no compatible release found")
}

func TestParseVersionCustomSuffix(t *testing.T) {
	require.Equal(t, [4]int{0, 1, 156, 0}, parseVersion("v0.1.156"))
	require.Equal(t, [4]int{0, 1, 156, 1}, parseVersion("v0.1.156-custom.1"))
	require.Equal(t, [4]int{0, 1, 156, 12}, parseVersion("0.1.156-custom.12"))
	// 旧部署版本格式:custom 后缀为日期序号,也按第 4 段解析
	require.Equal(t, [4]int{0, 1, 155, 20260714}, parseVersion("0.1.155-custom.20260714"))
	// 非 custom 的 semver 预发布后缀不计入第 4 段
	require.Equal(t, [4]int{0, 1, 156, 0}, parseVersion("v0.1.156-rc.1"))
	// 非法输入兜底为 0
	require.Equal(t, [4]int{0, 0, 0, 0}, parseVersion("garbage"))
}

func TestCompareVersionsCustomScheme(t *testing.T) {
	// 上游对上游(原有行为不变)
	require.Equal(t, -1, compareVersions("0.1.155", "0.1.156"))
	require.Equal(t, 0, compareVersions("0.1.156", "v0.1.156"))
	require.Equal(t, 1, compareVersions("0.1.157", "0.1.156"))
	// custom 对 custom
	require.Equal(t, -1, compareVersions("0.1.156-custom.1", "0.1.156-custom.2"))
	require.Equal(t, 0, compareVersions("0.1.156-custom.1", "0.1.156-custom.1"))
	// 上游基线 < 同基线 custom
	require.Equal(t, -1, compareVersions("0.1.156", "0.1.156-custom.1"))
	// 跨基线:上游新版 > 旧基线任意 custom
	require.Equal(t, -1, compareVersions("0.1.156-custom.9", "0.1.157"))
	// 现部署旧格式 < 新方案首版
	require.Equal(t, -1, compareVersions("0.1.155-custom.20260714", "0.1.156-custom.1"))
}

func TestCompareUpstreamBaseline(t *testing.T) {
	// same X.Y.Z baseline: custom is not behind upstream
	require.Equal(t, 0, compareUpstreamBaseline("0.1.160-custom.1", "0.1.160"))
	// behind official
	require.Equal(t, -1, compareUpstreamBaseline("0.1.156-custom.3", "0.1.160"))
	// ahead of official baseline
	require.Equal(t, 1, compareUpstreamBaseline("0.1.161-custom.1", "0.1.160"))
}

type dualRepoGitHubClientStub struct {
	selfRelease     *GitHubRelease
	upstreamRelease *GitHubRelease
}

func (s *dualRepoGitHubClientStub) FetchLatestRelease(_ context.Context, repo string) (*GitHubRelease, error) {
	switch repo {
	case githubRepo:
		return s.selfRelease, nil
	case upstreamGithubRepo:
		return s.upstreamRelease, nil
	default:
		return nil, errors.New("unexpected repo: " + repo)
	}
}

func (s *dualRepoGitHubClientStub) FetchRecentReleases(context.Context, string, int) ([]*GitHubRelease, error) {
	return nil, nil
}

func (s *dualRepoGitHubClientStub) DownloadFile(context.Context, string, string, int64) error {
	panic("unexpected DownloadFile")
}

func (s *dualRepoGitHubClientStub) FetchChecksumFile(context.Context, string) ([]byte, error) {
	panic("unexpected FetchChecksumFile")
}

func TestUpdateServiceCheckUpdateIncludesUpstreamRelease(t *testing.T) {
	client := &dualRepoGitHubClientStub{
		selfRelease: &GitHubRelease{
			TagName: "v0.1.160-custom.1",
			Name:    "v0.1.160-custom.1",
			HTMLURL: "https://github.com/Kline-x/sub2api-investigation/releases/tag/v0.1.160-custom.1",
		},
		upstreamRelease: &GitHubRelease{
			TagName: "v0.1.161",
			Name:    "v0.1.161",
			Body:    "upstream notes",
			HTMLURL: "https://github.com/Wei-Shaw/sub2api/releases/tag/v0.1.161",
		},
	}
	svc := NewUpdateService(&updateServiceCacheStub{}, client, "0.1.160-custom.1", "release")

	info, err := svc.CheckUpdate(context.Background(), true)

	require.NoError(t, err)
	require.Equal(t, "0.1.160-custom.1", info.LatestVersion)
	require.False(t, info.HasUpdate)
	require.Equal(t, "0.1.161", info.UpstreamLatestVersion)
	require.True(t, info.UpstreamHasUpdate)
	require.NotNil(t, info.UpstreamReleaseInfo)
	require.Equal(t, "https://github.com/Wei-Shaw/sub2api/releases/tag/v0.1.161", info.UpstreamReleaseInfo.HTMLURL)
}

func TestUpdateServiceCheckUpdateSameUpstreamBaselineNotBehind(t *testing.T) {
	client := &dualRepoGitHubClientStub{
		selfRelease: &GitHubRelease{
			TagName: "v0.1.160-custom.1",
			Name:    "v0.1.160-custom.1",
		},
		upstreamRelease: &GitHubRelease{
			TagName: "v0.1.160",
			Name:    "v0.1.160",
			HTMLURL: "https://github.com/Wei-Shaw/sub2api/releases/tag/v0.1.160",
		},
	}
	svc := NewUpdateService(&updateServiceCacheStub{}, client, "0.1.160-custom.1", "release")

	info, err := svc.CheckUpdate(context.Background(), true)

	require.NoError(t, err)
	require.Equal(t, "0.1.160", info.UpstreamLatestVersion)
	require.False(t, info.UpstreamHasUpdate)
}
