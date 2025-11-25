package version

import (
	"runtime"
	"time"

	"qcc_plus/internal/timeutil"
)

// Version information injected at build time via -ldflags.
var (
	Version   = "dev"
	GitCommit = ""
	BuildDate = ""
	GoVersion = runtime.Version()
)

// Info represents build and runtime version metadata.
type Info struct {
	Version          string `json:"version"`
	GitCommit        string `json:"git_commit"`
	BuildDate        string `json:"build_date"`
	BuildDateBeijing string `json:"build_date_beijing"`
	GoVersion        string `json:"go_version"`
}

// GetFormattedBuildDate returns the build time formatted in Beijing time.
// BuildDate is expected to be an RFC3339 string in UTC set at build time.
func GetFormattedBuildDate() string {
	switch BuildDate {
	case "":
		return "未知"
	case "dev":
		return "开发版本"
	}

	t, err := time.Parse(time.RFC3339, BuildDate)
	if err != nil {
		return BuildDate + " (格式错误)"
	}

	return timeutil.FormatBeijingTime(t)
}

// GetVersionInfo returns the current version metadata.
func GetVersionInfo() Info {
	return Info{
		Version:          Version,
		GitCommit:        GitCommit,
		BuildDate:        BuildDate,
		BuildDateBeijing: GetFormattedBuildDate(),
		GoVersion:        GoVersion,
	}
}
