package agentdetect

import "sort"

type Source int

const (
	SourcePath Source = iota
	SourceEnv
	SourceBoth
)

func (s Source) String() string {
	switch s {
	case SourcePath:
		return "PATH"
	case SourceEnv:
		return "env"
	case SourceBoth:
		return "PATH+env"
	default:
		return "?"
	}
}

type DetectedAgent struct {
	Name           string
	Source         Source
	BinaryPath     string
	EnvHost        string
	SuggestedHosts []string
}

type Config struct {
	PathLookup func(string) (string, error)
	EnvGetter  func(string) string
	IDNLookup  func(string) (string, error)
}

// Run performs agent detection. Never returns an error — detection is
// best-effort and silent on failure.
func Run(cfg Config) []DetectedAgent {
	type accum struct {
		binaryPath string
		envHost    string
	}
	agents := map[string]*accum{}

	for _, name := range KnownBinaries {
		path, err := cfg.PathLookup(name)
		if err != nil || path == "" {
			continue
		}
		agents[name] = &accum{binaryPath: path}
	}

	for _, b := range KnownEnvVars {
		raw := cfg.EnvGetter(b.Var)
		if raw == "" {
			continue
		}
		host := extractHost(raw, cfg.IDNLookup)
		if host == "" {
			continue
		}
		a, ok := agents[b.Agent]
		if !ok {
			a = &accum{}
			agents[b.Agent] = a
		}
		if a.envHost == "" {
			a.envHost = host
		}
	}

	names := make([]string, 0, len(agents))
	for name := range agents {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]DetectedAgent, 0, len(names))
	for _, name := range names {
		a := agents[name]
		var src Source
		switch {
		case a.binaryPath != "" && a.envHost != "":
			src = SourceBoth
		case a.binaryPath != "":
			src = SourcePath
		case a.envHost != "":
			src = SourceEnv
		}
		var hosts []string
		if a.envHost != "" {
			hosts = []string{a.envHost}
		} else if defs, ok := Defaults[name]; ok {
			hosts = append([]string(nil), defs...)
		}
		out = append(out, DetectedAgent{
			Name:           name,
			Source:         src,
			BinaryPath:     a.binaryPath,
			EnvHost:        a.envHost,
			SuggestedHosts: hosts,
		})
	}
	return out
}
