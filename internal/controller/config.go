package controller

var (
	environment                          string
	providerName                         string
	accessRequestServiceAccountNamespace string
)

// SetEnvironment sets the environment
func SetEnvironment(env string) {
	if environment != "" {
		panic("environment already set")
	}
	environment = env
}

// Environment retrieves the environment
func Environment() string {
	if environment == "" {
		panic("environment not set")
	}
	return environment
}

// SetProviderName sets the provider name
func SetProviderName(name string) {
	if providerName != "" {
		panic("provider name already set")
	}
	providerName = name
}

// ProviderName retrieves the provider name
func ProviderName() string {
	if providerName == "" {
		panic("provider name not set")
	}
	return providerName
}

// SetAccessRequestServiceAccountNamespace sets the service account namespace
func SetAccessRequestServiceAccountNamespace(ns string) {
	if accessRequestServiceAccountNamespace != "" {
		panic("accessrequest namespace already set")
	}
	accessRequestServiceAccountNamespace = ns
}

// AccessRequestServiceAccountNamespace retrieves the service account namespace
func AccessRequestServiceAccountNamespace() string {
	if accessRequestServiceAccountNamespace == "" {
		panic("accessrequest namespace not set")
	}
	return accessRequestServiceAccountNamespace
}
