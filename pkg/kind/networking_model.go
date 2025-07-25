package kind

// IPAMConfig represents individual configurations within the IPAMConfig array.
type IPAMConfig struct {
	Subnet  string `json:"Subnet"`
	Gateway string `json:"Gateway"`
}

// IPAM represents IP address management configuration.
type IPAM struct {
	Driver  string            `json:"Driver"`
	Options map[string]string `json:"Options"`
	Config  []IPAMConfig      `json:"Config"`
}

// Container represents a container within the network.
type Container struct {
	Name        string `json:"Name"`
	EndpointID  string `json:"EndpointID"`
	MacAddress  string `json:"MacAddress"`
	IPv4Address string `json:"IPv4Address"`
	IPv6Address string `json:"IPv6Address"`
}

// ConfigFrom represents the source network for configuration data.
type ConfigFrom struct {
	Network string `json:"Network"`
}

// Network represents the network configuration struct.
type Network struct {
	Name       string               `json:"Name"`
	ID         string               `json:"ID"`
	Created    string               `json:"Created"`
	Scope      string               `json:"Scope"`
	Driver     string               `json:"Driver"`
	EnableIPv6 bool                 `json:"EnableIPv6"`
	IPAM       IPAM                 `json:"IPAM"`
	Internal   bool                 `json:"Internal"`
	Attachable bool                 `json:"Attachable"`
	Ingress    bool                 `json:"Ingress"`
	ConfigFrom ConfigFrom           `json:"ConfigFrom"`
	ConfigOnly bool                 `json:"ConfigOnly"`
	Containers map[string]Container `json:"Containers"`
	Options    map[string]string    `json:"Options"`
	Labels     map[string]string    `json:"Labels"`
}
