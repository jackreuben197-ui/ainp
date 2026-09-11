package buildinfo

import "runtime/debug"

type Info struct {
	Revision string
	Time     string
	Modified bool
	Go       string
}

func Current() Info {
	info := Info{}
	build, ok := debug.ReadBuildInfo()
	if !ok {
		return info
	}
	info.Go = build.GoVersion
	for _, setting := range build.Settings {
		switch setting.Key {
		case "vcs.revision":
			info.Revision = setting.Value
		case "vcs.time":
			info.Time = setting.Value
		case "vcs.modified":
			info.Modified = setting.Value == "true"
		}
	}
	return info
}
