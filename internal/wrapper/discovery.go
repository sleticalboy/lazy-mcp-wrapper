package wrapper

type DiscoveryInfo struct {
	ServerInfo     discoveryServerInfo    `json:"serverInfo"`
	Protocol       discoveryProtocolInfo  `json:"protocol"`
	Capabilities   discoveryCapabilities  `json:"capabilities"`
	Lifecycle      discoveryLifecycleInfo `json:"lifecycle"`
	Upstream       discoveryUpstreamInfo  `json:"upstream"`
	Cache          discoveryCacheInfo     `json:"cache"`
	StartsUpstream bool                   `json:"starts_upstream"`
	Experimental   bool                   `json:"experimental"`
}

type discoveryServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type discoveryProtocolInfo struct {
	Mode             string   `json:"mode"`
	ClientModes      []string `json:"client_modes"`
	UpstreamMode     string   `json:"upstream_mode"`
	LegacyInitialize bool     `json:"legacy_initialize"`
	StatelessInbound bool     `json:"stateless_inbound"`
	ProtocolVersion  string   `json:"protocolVersion"`
}

type discoveryCapabilities struct {
	Tools     discoveryMethodCapability `json:"tools"`
	Prompts   discoveryMethodCapability `json:"prompts"`
	Resources discoveryMethodCapability `json:"resources"`
	Ping      bool                      `json:"ping"`
	Discover  bool                      `json:"discover"`
}

type discoveryMethodCapability struct {
	List      bool `json:"list,omitempty"`
	Call      bool `json:"call,omitempty"`
	Get       bool `json:"get,omitempty"`
	Read      bool `json:"read,omitempty"`
	Templates bool `json:"templates,omitempty"`
}

type discoveryLifecycleInfo struct {
	Sharing string `json:"sharing"`
}

type discoveryUpstreamInfo struct {
	Type        string `json:"type"`
	Protocol    string `json:"protocol,omitempty"`
	HTTPBackend string `json:"http_backend,omitempty"`
	Auth        string `json:"auth,omitempty"`
}

type discoveryCacheInfo struct {
	ToolsList bool      `json:"tools_list"`
	Info      CacheInfo `json:"info"`
}

func (p *Proxy) discovery() DiscoveryInfo {
	protocolVersion := p.protocolVersion()
	upstreamType := "stdio"
	upstreamProtocol := ""
	upstreamMode := "legacy-initialize"
	if p.cfg.URL != "" {
		upstreamType = "http"
		upstreamProtocol = p.cfg.HTTPProtocol()
		upstreamMode = p.cfg.UpstreamProtocolMode()
		if upstreamMode == "legacy" {
			upstreamMode = "legacy-initialize"
		}
		if upstreamMode == "auto" && !p.cfg.StatelessHTTPUpstream() {
			upstreamMode = "legacy-initialize"
		}
	}
	auth := p.cfg.Auth
	if auth == "" {
		auth = "none"
	}
	return DiscoveryInfo{
		ServerInfo: discoveryServerInfo{
			Name:    "lazy-mcp-wrapper/" + p.cfg.Name,
			Version: "0.1.0",
		},
		Protocol: discoveryProtocolInfo{
			Mode:             "bridge",
			ClientModes:      []string{"legacy-initialize", "stateless-inbound"},
			UpstreamMode:     upstreamMode,
			LegacyInitialize: true,
			StatelessInbound: true,
			ProtocolVersion:  protocolVersion,
		},
		Capabilities: discoveryCapabilities{
			Tools:     discoveryMethodCapability{List: true, Call: true},
			Prompts:   discoveryMethodCapability{List: true, Get: true},
			Resources: discoveryMethodCapability{List: true, Read: true, Templates: true},
			Ping:      true,
			Discover:  true,
		},
		Lifecycle: discoveryLifecycleInfo{
			Sharing: p.cfg.Sharing,
		},
		Upstream: discoveryUpstreamInfo{
			Type:        upstreamType,
			Protocol:    upstreamProtocol,
			HTTPBackend: p.cfg.HTTPBackend,
			Auth:        auth,
		},
		Cache: discoveryCacheInfo{
			ToolsList: !p.cfg.DisableCache,
			Info:      p.cfg.CacheInfo(),
		},
		StartsUpstream: false,
		Experimental:   true,
	}
}
