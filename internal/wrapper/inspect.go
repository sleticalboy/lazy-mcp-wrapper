package wrapper

type InspectInfo struct {
	Name           string            `json:"name"`
	Command        string            `json:"command"`
	Args           []string          `json:"args"`
	Env            map[string]string `json:"env,omitempty"`
	CWD            string            `json:"cwd,omitempty"`
	RealProtocol   string            `json:"real_protocol_version"`
	RealFraming    string            `json:"real_framing"`
	IdleTimeout    string            `json:"idle_timeout"`
	StartupTimeout string            `json:"startup_timeout"`
	CallTimeout    string            `json:"call_timeout"`
	LogFile        string            `json:"log_file,omitempty"`
	Cache          CacheInfo         `json:"cache"`
}

func Inspect(cfg Config) InspectInfo {
	framing, _ := cfg.Framing()
	protocol := cfg.RealProtocol
	if protocol == "" {
		protocol = "2024-11-05"
	}
	return InspectInfo{
		Name:           cfg.Name,
		Command:        cfg.Command,
		Args:           redactArgs(cfg.Args),
		Env:            redactEnv(cfg.Env),
		CWD:            cfg.CWD,
		RealProtocol:   protocol,
		RealFraming:    string(framing),
		IdleTimeout:    cfg.IdleTimeout.String(),
		StartupTimeout: cfg.StartupTimeout.String(),
		CallTimeout:    cfg.CallTimeout.String(),
		LogFile:        cfg.LogFile,
		Cache:          cfg.CacheInfo(),
	}
}
